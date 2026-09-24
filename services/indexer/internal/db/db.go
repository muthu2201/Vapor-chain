// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary and confidential. See LICENSE at the repository root.
// Provenance: VAPOR-6eabb1be532bdef4

// Package db is the indexer's PostgreSQL schema and write path. Validators keep
// only days of history; this database (plus the Parquet archive) keeps
// everything, which is why explorers and developer dashboards read from here
// and never from validators.
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const Schema = `
CREATE TABLE IF NOT EXISTS blocks (
  height      BIGINT PRIMARY KEY,
  hash        BYTEA NOT NULL,
  time        TIMESTAMPTZ NOT NULL,
  tx_count    INT NOT NULL,
  gas_used    BIGINT NOT NULL,
  gas_limit   BIGINT NOT NULL,
  base_fee    NUMERIC(80,0),
  proposer    BYTEA
);
CREATE TABLE IF NOT EXISTS evm_txs (
  hash        BYTEA PRIMARY KEY,
  height      BIGINT NOT NULL REFERENCES blocks(height),
  idx         INT NOT NULL,
  sender      BYTEA NOT NULL,
  recipient   BYTEA,
  value       NUMERIC(80,0) NOT NULL,
  gas_used    BIGINT NOT NULL,
  status      SMALLINT NOT NULL,
  tx_type     SMALLINT NOT NULL,
  contract    BYTEA
);
CREATE INDEX IF NOT EXISTS evm_txs_sender ON evm_txs (sender, height);
CREATE INDEX IF NOT EXISTS evm_txs_recipient ON evm_txs (recipient, height);
CREATE TABLE IF NOT EXISTS evm_logs (
  height      BIGINT NOT NULL,
  tx_hash     BYTEA NOT NULL,
  log_index   INT NOT NULL,
  address     BYTEA NOT NULL,
  topic0      BYTEA,
  topic1      BYTEA,
  topic2      BYTEA,
  topic3      BYTEA,
  data        BYTEA,
  PRIMARY KEY (height, log_index)
);
CREATE INDEX IF NOT EXISTS evm_logs_addr_topic ON evm_logs (address, topic0, height);
CREATE TABLE IF NOT EXISTS settlements (
  height          BIGINT NOT NULL,
  seq             INT NOT NULL,
  time            TIMESTAMPTZ NOT NULL,
  app_id          BIGINT NOT NULL,
  payer           TEXT NOT NULL,
  payee           TEXT NOT NULL,
  denom           TEXT NOT NULL,
  amount          NUMERIC(80,0) NOT NULL,
  fee             NUMERIC(80,0) NOT NULL,
  net             NUMERIC(80,0) NOT NULL,
  app_share       NUMERIC(80,0) NOT NULL,
  referrer        TEXT,
  referrer_share  NUMERIC(80,0) NOT NULL,
  validator_share NUMERIC(80,0) NOT NULL,
  relayer_share   NUMERIC(80,0) NOT NULL,
  treasury_share  NUMERIC(80,0) NOT NULL,
  PRIMARY KEY (height, seq)
);
CREATE INDEX IF NOT EXISTS settlements_app_time ON settlements (app_id, time);
CREATE INDEX IF NOT EXISTS settlements_payer ON settlements (payer);
CREATE TABLE IF NOT EXISTS app_events (
  height   BIGINT NOT NULL,
  seq      INT NOT NULL,
  kind     TEXT NOT NULL,
  app_id   BIGINT,
  attrs    JSONB NOT NULL,
  PRIMARY KEY (height, seq)
);
CREATE INDEX IF NOT EXISTS app_events_app ON app_events (app_id, height);
CREATE TABLE IF NOT EXISTS sponsored_ops (
  height        BIGINT NOT NULL,
  log_index     INT NOT NULL,
  app_id        BIGINT NOT NULL,
  sender        BYTEA NOT NULL,
  user_op_hash  BYTEA NOT NULL,
  gas_cost      NUMERIC(80,0) NOT NULL,
  fee_per_gas   NUMERIC(80,0) NOT NULL,
  PRIMARY KEY (height, log_index)
);
CREATE INDEX IF NOT EXISTS sponsored_ops_app ON sponsored_ops (app_id, height);
CREATE TABLE IF NOT EXISTS indexer_cursor (
  name   TEXT PRIMARY KEY,
  height BIGINT NOT NULL
);`

type DB struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, Schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (d *DB) Close() { d.Pool.Close() }

func (d *DB) Cursor(ctx context.Context, name string) (int64, error) {
	var h int64
	err := d.Pool.QueryRow(ctx, `SELECT height FROM indexer_cursor WHERE name=$1`, name).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return h, err
}

func SetCursor(ctx context.Context, tx pgx.Tx, name string, h int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO indexer_cursor(name,height) VALUES ($1,$2) ON CONFLICT (name) DO UPDATE SET height=EXCLUDED.height`, name, h)
	return err
}
