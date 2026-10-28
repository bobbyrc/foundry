package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations are applied in order; migration i (0-based) has version i+1 and is never edited
// once shipped. Append new schema changes at the end.
var migrations = []string{
	`CREATE TABLE tasks (
		id TEXT PRIMARY KEY,
		project TEXT NOT NULL,
		status TEXT NOT NULL,
		kind TEXT NOT NULL,
		parent_id TEXT NOT NULL DEFAULT '',
		source_ref TEXT NOT NULL DEFAULT '',
		priority INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		data TEXT NOT NULL
	);
	CREATE INDEX tasks_status_priority ON tasks (status, priority, created_at);
	CREATE INDEX tasks_project_source_ref ON tasks (project, source_ref);
	CREATE INDEX tasks_parent_id ON tasks (parent_id);

	CREATE TABLE runs (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		project TEXT NOT NULL,
		status TEXT NOT NULL,
		started_at INTEGER NOT NULL,
		data TEXT NOT NULL
	);
	CREATE INDEX runs_task_id ON runs (task_id, started_at);
	CREATE INDEX runs_project_started ON runs (project, started_at);

	CREATE TABLE run_logs (
		run_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		time INTEGER NOT NULL,
		kind TEXT NOT NULL,
		text TEXT NOT NULL,
		PRIMARY KEY (run_id, seq)
	);

	CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		time INTEGER NOT NULL,
		type TEXT NOT NULL,
		project TEXT NOT NULL DEFAULT '',
		task_id TEXT NOT NULL DEFAULT '',
		run_id TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		data TEXT
	);
	CREATE INDEX events_task_id ON events (task_id);
	CREATE INDEX events_project ON events (project);

	CREATE TABLE approvals (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		task_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		data TEXT NOT NULL
	);
	CREATE INDEX approvals_status_created ON approvals (status, created_at);

	CREATE TABLE deployments (
		id TEXT PRIMARY KEY,
		project TEXT NOT NULL,
		state TEXT NOT NULL,
		started_at INTEGER NOT NULL,
		data TEXT NOT NULL
	);
	CREATE INDEX deployments_project_state ON deployments (project, state, started_at);

	CREATE TABLE signals (
		id TEXT PRIMARY KEY,
		project TEXT NOT NULL,
		source TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		data TEXT NOT NULL,
		UNIQUE (project, source, fingerprint)
	);
	CREATE INDEX signals_project_updated ON signals (project, updated_at);

	CREATE TABLE friction (
		id TEXT PRIMARY KEY,
		project TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		data TEXT NOT NULL,
		UNIQUE (project, fingerprint)
	);
	CREATE INDEX friction_project_updated ON friction (project, updated_at);

	CREATE TABLE conversations (
		id TEXT PRIMARY KEY,
		updated_at INTEGER NOT NULL,
		data TEXT NOT NULL
	);

	CREATE TABLE chat_messages (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		data TEXT NOT NULL
	);
	CREATE INDEX chat_messages_conversation ON chat_messages (conversation_id, created_at);

	CREATE TABLE loop_state (
		name TEXT NOT NULL,
		project TEXT NOT NULL,
		data TEXT NOT NULL,
		PRIMARY KEY (name, project)
	);`,
	`CREATE TABLE users (
		username TEXT PRIMARY KEY,
		role TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE INDEX sessions_username ON sessions (username);
	CREATE INDEX sessions_expires_at ON sessions (expires_at);`,
}

// migrate applies every migration whose version is not yet recorded, each in its own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	for i, m := range migrations {
		version := i + 1
		var applied int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		err := withTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
	}
	return nil
}
