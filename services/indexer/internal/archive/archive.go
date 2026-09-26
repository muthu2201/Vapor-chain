// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

// Package archive writes immutable Parquet segments (blocks, txs, logs,
// settlements) of every ArchiveEvery blocks plus a manifest with SHA-256
// checksums. Point ArchiveDir at object storage (s3fs/rclone mount or a
// bucket-synced volume) with lifecycle tiering: anyone can mirror it, and
// with genesis + every upgrade binary the chain is fully reconstructable.
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/muthu2201/vapor-chain/services/indexer/internal/db"
)

var mArchived = promauto.NewGauge(prometheus.GaugeOpts{Name: "vapor_indexer_archived_height", Help: "Highest height sealed into a Parquet segment"})

type BlockRow struct {
	Height   int64     `parquet:"height"`
	Hash     []byte    `parquet:"hash"`
	Time     time.Time `parquet:"time,timestamp"`
	TxCount  int32     `parquet:"tx_count"`
	GasUsed  int64     `parquet:"gas_used"`
	GasLimit int64     `parquet:"gas_limit"`
}

type TxRow struct {
	Hash      []byte `parquet:"hash"`
	Height    int64  `parquet:"height"`
	Idx       int32  `parquet:"idx"`
	Sender    []byte `parquet:"sender"`
	Recipient []byte `parquet:"recipient,optional"`
	Value     string `parquet:"value"`
	GasUsed   int64  `parquet:"gas_used"`
	Status    int32  `parquet:"status"`
	TxType    int32  `parquet:"tx_type"`
}

type LogRow struct {
	Height   int64  `parquet:"height"`
	TxHash   []byte `parquet:"tx_hash"`
	LogIndex int32  `parquet:"log_index"`
	Address  []byte `parquet:"address"`
	Topic0   []byte `parquet:"topic0,optional"`
	Topic1   []byte `parquet:"topic1,optional"`
	Topic2   []byte `parquet:"topic2,optional"`
	Topic3   []byte `parquet:"topic3,optional"`
	Data     []byte `parquet:"data,optional"`
}

type SettlementRow struct {
	Height int64     `parquet:"height"`
	Seq    int32     `parquet:"seq"`
	Time   time.Time `parquet:"time,timestamp"`
	AppID  int64     `parquet:"app_id"`
	Payer  string    `parquet:"payer"`
	Payee  string    `parquet:"payee"`
	Denom  string    `parquet:"denom"`
	Amount string    `parquet:"amount"`
	Fee    string    `parquet:"fee"`
}

type Manifest struct {
	From       int64             `json:"from_height"`
	To         int64             `json:"to_height"`
	CreatedAt  time.Time         `json:"created_at"`
	Files      map[string]string `json:"sha256"`
	Rows       map[string]int    `json:"rows"`
	Provenance string            `json:"provenance"`
}

type Archiver struct {
	db    *db.DB
	dir   string
	every int64
	log   *slog.Logger
}

func New(d *db.DB, dir string, every int64, log *slog.Logger) *Archiver {
	return &Archiver{db: d, dir: dir, every: every, log: log}
}

func writeParquet[T any](path string, rows []T) (string, error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	w := parquet.NewGenericWriter[T](f, parquet.Compression(&parquet.Zstd))
	if _, err := w.Write(rows); err != nil {
		f.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	g, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer g.Close()
	h := sha256.New()
	if _, err := io.Copy(h, g); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Seal writes blocks [from, to] as a Parquet segment with a sha256 manifest.
// Idempotent: a segment whose manifest exists is left untouched.
func (a *Archiver) Seal(ctx context.Context, from, to int64) error {
	seg := filepath.Join(a.dir, fmt.Sprintf("%012d-%012d", from, to))
	if _, err := os.Stat(filepath.Join(seg, "manifest.json")); err == nil {
		return nil // already sealed
	}
	if err := os.MkdirAll(seg, 0o755); err != nil {
		return err
	}
	var blocks []BlockRow
	var txs []TxRow
	var logs []LogRow
	var sets []SettlementRow
	q := func(sql string, scan func(pgx.Rows) error) error {
		rows, err := a.db.Pool.Query(ctx, sql, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := q(`SELECT height,hash,time,tx_count,gas_used,gas_limit FROM blocks WHERE height BETWEEN $1 AND $2 ORDER BY height`, func(r pgx.Rows) error {
		var b BlockRow
		if err := r.Scan(&b.Height, &b.Hash, &b.Time, &b.TxCount, &b.GasUsed, &b.GasLimit); err != nil {
			return err
		}
		blocks = append(blocks, b)
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT hash,height,idx,sender,recipient,value::TEXT,gas_used,status,tx_type FROM evm_txs WHERE height BETWEEN $1 AND $2 ORDER BY height,idx`, func(r pgx.Rows) error {
		var t TxRow
		var st, tt int16
		if err := r.Scan(&t.Hash, &t.Height, &t.Idx, &t.Sender, &t.Recipient, &t.Value, &t.GasUsed, &st, &tt); err != nil {
			return err
		}
		t.Status, t.TxType = int32(st), int32(tt)
		txs = append(txs, t)
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT height,tx_hash,log_index,address,topic0,topic1,topic2,topic3,data FROM evm_logs WHERE height BETWEEN $1 AND $2 ORDER BY height,log_index`, func(r pgx.Rows) error {
		var l LogRow
		if err := r.Scan(&l.Height, &l.TxHash, &l.LogIndex, &l.Address, &l.Topic0, &l.Topic1, &l.Topic2, &l.Topic3, &l.Data); err != nil {
			return err
		}
		logs = append(logs, l)
		return nil
	}); err != nil {
		return err
	}
	if err := q(`SELECT height,seq,time,app_id,payer,payee,denom,amount::TEXT,fee::TEXT FROM settlements WHERE height BETWEEN $1 AND $2 ORDER BY height,seq`, func(r pgx.Rows) error {
		var s SettlementRow
		if err := r.Scan(&s.Height, &s.Seq, &s.Time, &s.AppID, &s.Payer, &s.Payee, &s.Denom, &s.Amount, &s.Fee); err != nil {
			return err
		}
		sets = append(sets, s)
		return nil
	}); err != nil {
		return err
	}
	m := Manifest{From: from, To: to, CreatedAt: time.Now().UTC(), Files: map[string]string{}, Rows: map[string]int{},
		Provenance: "VAPOR-6eabb1be532bdef4"}
	var err error
	if m.Files["blocks.parquet"], err = writeParquet(filepath.Join(seg, "blocks.parquet"), blocks); err != nil {
		return err
	}
	if m.Files["txs.parquet"], err = writeParquet(filepath.Join(seg, "txs.parquet"), txs); err != nil {
		return err
	}
	if m.Files["logs.parquet"], err = writeParquet(filepath.Join(seg, "logs.parquet"), logs); err != nil {
		return err
	}
	if m.Files["settlements.parquet"], err = writeParquet(filepath.Join(seg, "settlements.parquet"), sets); err != nil {
		return err
	}
	m.Rows = map[string]int{"blocks": len(blocks), "txs": len(txs), "logs": len(logs), "settlements": len(sets)}
	bz, _ := json.MarshalIndent(m, "", "  ")
	// the manifest is written last: its presence marks a complete segment
	return os.WriteFile(filepath.Join(seg, "manifest.json"), bz, 0o644)
}

// Run seals every complete segment below the indexed height.
func (a *Archiver) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		idx, err := a.db.Cursor(ctx, "blocks")
		if err != nil {
			continue
		}
		done, _ := a.db.Cursor(ctx, "archive")
		for done+a.every <= idx {
			from, to := done+1, done+a.every
			if err := a.Seal(ctx, from, to); err != nil {
				a.log.Error("archive seal failed", "from", from, "to", to, "err", err)
				break
			}
			if err := pgx.BeginFunc(ctx, a.db.Pool, func(tx pgx.Tx) error { return db.SetCursor(ctx, tx, "archive", to) }); err != nil {
				break
			}
			done = to
			mArchived.Set(float64(to))
			a.log.Info("sealed archive segment", "from", from, "to", to)
		}
	}
}
