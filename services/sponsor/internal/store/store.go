// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package store persists sponsorship accounting in PostgreSQL. Reservations
// are taken atomically (single UPDATE ... WHERE used + gas <= quota) so two
// concurrent requests can never both squeeze past an app's quota.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS sponsor_app_usage (
  app_id      BIGINT NOT NULL,
  epoch       BIGINT NOT NULL,
  gas_used    NUMERIC(40,0) NOT NULL DEFAULT 0,
  ops         BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (app_id, epoch)
);
CREATE TABLE IF NOT EXISTS sponsor_sender_daily (
  sender      BYTEA NOT NULL,
  day         DATE NOT NULL,
  ops         INT NOT NULL DEFAULT 0,
  PRIMARY KEY (sender, day)
);
CREATE TABLE IF NOT EXISTS sponsor_signatures (
  id          BIGSERIAL PRIMARY KEY,
  app_id      BIGINT NOT NULL,
  epoch       BIGINT NOT NULL,
  sender      BYTEA NOT NULL,
  nonce       NUMERIC(80,0) NOT NULL,
  gas_limit   NUMERIC(40,0) NOT NULL,
  valid_until BIGINT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  settled_gas NUMERIC(40,0),
  UNIQUE (sender, nonce, app_id)
);
CREATE INDEX IF NOT EXISTS sponsor_signatures_open ON sponsor_signatures (valid_until) WHERE settled_gas IS NULL;
CREATE TABLE IF NOT EXISTS sponsor_cursor (
  name TEXT PRIMARY KEY,
  block BIGINT NOT NULL
);`

type Store struct{ pool *pgxpool.Pool }

var (
	ErrQuotaExceeded = errors.New("app sponsorship quota exhausted for this epoch")
	ErrSenderCap     = errors.New("sender reached its daily sponsored-operation cap")
	ErrAlreadySigned = errors.New("this operation was already sponsored")
)

func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Reservation describes one signing attempt.
type Reservation struct {
	AppID      uint64
	Epoch      uint64
	Quota      uint64
	Sender     []byte
	Nonce      string
	Gas        uint64
	SenderCap  int
	ValidUntil int64
}

// Reserve atomically debits the app quota and the sender's daily cap and
// records the signature. All-or-nothing in one transaction.
func (s *Store) Reserve(ctx context.Context, r Reservation) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO sponsor_app_usage(app_id, epoch) VALUES ($1,$2) ON CONFLICT DO NOTHING`, r.AppID, r.Epoch); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE sponsor_app_usage SET gas_used = gas_used + $3, ops = ops + 1
			WHERE app_id=$1 AND epoch=$2 AND gas_used + $3 <= $4`, r.AppID, r.Epoch, r.Gas, r.Quota)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrQuotaExceeded
		}
		day := time.Now().UTC().Format("2006-01-02")
		if _, err := tx.Exec(ctx, `INSERT INTO sponsor_sender_daily(sender, day) VALUES ($1,$2) ON CONFLICT DO NOTHING`, r.Sender, day); err != nil {
			return err
		}
		tag, err = tx.Exec(ctx, `UPDATE sponsor_sender_daily SET ops = ops + 1 WHERE sender=$1 AND day=$2 AND ops < $3`, r.Sender, day, r.SenderCap)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrSenderCap
		}
		tag, err = tx.Exec(ctx, `INSERT INTO sponsor_signatures(app_id, epoch, sender, nonce, gas_limit, valid_until)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (sender, nonce, app_id) DO NOTHING`,
			r.AppID, r.Epoch, r.Sender, r.Nonce, r.Gas, r.ValidUntil)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrAlreadySigned
		}
		return nil
	})
}

// Settle records the actual gas of a sponsored op (from the paymaster's
// Sponsored event) and refunds the difference to the app's epoch usage.
func (s *Store) Settle(ctx context.Context, appID uint64, sender []byte, actualGas uint64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		var epoch uint64
		var limit uint64
		err := tx.QueryRow(ctx, `SELECT id, epoch, gas_limit::BIGINT FROM sponsor_signatures
			WHERE app_id=$1 AND sender=$2 AND settled_gas IS NULL ORDER BY id LIMIT 1 FOR UPDATE`, appID, sender).Scan(&id, &epoch, &limit)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sponsor_signatures SET settled_gas=$2 WHERE id=$1`, id, actualGas); err != nil {
			return err
		}
		if actualGas < limit {
			_, err = tx.Exec(ctx, `UPDATE sponsor_app_usage SET gas_used = GREATEST(gas_used - $3, 0) WHERE app_id=$1 AND epoch=$2`, appID, epoch, limit-actualGas)
		}
		return err
	})
}

// ExpireUnused releases reservations whose signature expired unused.
func (s *Store) ExpireUnused(ctx context.Context, now int64) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE sponsor_signatures SET settled_gas = 0
			WHERE settled_gas IS NULL AND valid_until < $1 RETURNING app_id, epoch, gas_limit::BIGINT`, now)
		if err != nil {
			return err
		}
		type rel struct{ app, epoch, gas uint64 }
		var rels []rel
		for rows.Next() {
			var r rel
			if err := rows.Scan(&r.app, &r.epoch, &r.gas); err != nil {
				return err
			}
			rels = append(rels, r)
		}
		rows.Close()
		for _, r := range rels {
			if _, err := tx.Exec(ctx, `UPDATE sponsor_app_usage SET gas_used = GREATEST(gas_used - $3, 0) WHERE app_id=$1 AND epoch=$2`, r.app, r.epoch, r.gas); err != nil {
				return err
			}
		}
		n = int64(len(rels))
		return nil
	})
	return n, err
}

// Usage returns gas used by an app in an epoch.
func (s *Store) Usage(ctx context.Context, appID, epoch uint64) (uint64, int64, error) {
	var gas uint64
	var ops int64
	err := s.pool.QueryRow(ctx, `SELECT gas_used::BIGINT, ops FROM sponsor_app_usage WHERE app_id=$1 AND epoch=$2`, appID, epoch).Scan(&gas, &ops)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	return gas, ops, err
}

func (s *Store) Cursor(ctx context.Context, name string) (uint64, error) {
	var b uint64
	err := s.pool.QueryRow(ctx, `SELECT block FROM sponsor_cursor WHERE name=$1`, name).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return b, err
}

func (s *Store) SetCursor(ctx context.Context, name string, b uint64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sponsor_cursor(name, block) VALUES ($1,$2) ON CONFLICT (name) DO UPDATE SET block = EXCLUDED.block`, name, b)
	return err
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
