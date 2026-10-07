package store

import (
	"context"
	"database/sql"
	"fmt"
)

const ApplicationID = 1162037553
const SchemaVersion = 1

var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS entries (
	id TEXT PRIMARY KEY,
	axis TEXT NOT NULL,
	scope TEXT NOT NULL,
	at INTEGER NOT NULL,
	body TEXT NOT NULL,
	attrs TEXT NOT NULL
)`,
	`CREATE INDEX IF NOT EXISTS entries_axis_at ON entries(axis, at DESC)`,
	`CREATE INDEX IF NOT EXISTS entries_scope_axis_at ON entries(scope, axis, at DESC)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(body, content='entries', content_rowid='rowid', tokenize='trigram')`,
	`CREATE TRIGGER IF NOT EXISTS entries_fts_ai AFTER INSERT ON entries BEGIN
	INSERT INTO entries_fts(rowid, body) VALUES (new.rowid, new.body);
END`,
	`CREATE TRIGGER IF NOT EXISTS entries_fts_ad AFTER DELETE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
END`,
	`CREATE TRIGGER IF NOT EXISTS entries_fts_au AFTER UPDATE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
	INSERT INTO entries_fts(rowid, body) VALUES (new.rowid, new.body);
END`,
}

func migrate(ctx context.Context, db *sql.DB, from int) error {
	if from < 0 || from > SchemaVersion {
		return fmt.Errorf("schema version %d is out of range", from)
	}
	if from >= SchemaVersion {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, stmt := range schemaV1 {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}