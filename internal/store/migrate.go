package store

import (
	"context"
	"database/sql"
	"fmt"
)

// currentSchemaVersion 是当前程序支持的数据库模式版本.
const currentSchemaVersion = 1

// migration 描述一次数据库模式升级.
type migration struct {
	version int
	name    string
	apply   func(ctx context.Context, tx *sql.Tx) error
}

// schemaMigrations 按版本升序保存全部迁移, 新增版本时追加到末尾.
var schemaMigrations = []migration{
	{version: 1, name: "init", apply: migrateV1},
}

// migrate 把数据库升级到 currentSchemaVersion.
// 版本高于程序支持值时拒绝启动, 每个迁移在独立事务内执行并写入 user_version,
// 任一迁移失败则回滚该迁移并停止后续升级.
func migrate(ctx context.Context, db *sql.DB) error {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("读取数据库模式版本失败: %w", err)
	}
	if current > currentSchemaVersion {
		return fmt.Errorf("数据库模式版本 %d 高于程序支持的版本 %d, 请升级程序", current, currentSchemaVersion)
	}
	for _, m := range schemaMigrations {
		if m.version <= current {
			continue
		}
		if err := runMigration(ctx, db, m); err != nil {
			return fmt.Errorf("数据库迁移 v%d (%s) 失败: %w", m.version, m.name, err)
		}
	}
	return nil
}

// runMigration 在单个事务内执行一个迁移, 并把 user_version 推进到该版本.
func runMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// 提交成功后的回滚是空操作.
	defer func() { _ = tx.Rollback() }()
	if err := m.apply(ctx, tx); err != nil {
		return err
	}
	// PRAGMA 不支持参数占位符, 版本号来自内置迁移表.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV1 建立 v1 模式.
func migrateV1(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range schemaV1 {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// schemaV1 是 v1 的建表语句, 顺序与索引依赖保持一致.
var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS rooms (
		id         INTEGER PRIMARY KEY,
		room_no    TEXT NOT NULL,
		elcarea    INTEGER NOT NULL CHECK (elcarea >= 0),
		elcbuis    TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		UNIQUE (room_no, elcarea, elcbuis)
	)`,
	`CREATE TABLE IF NOT EXISTS samples (
		id      INTEGER PRIMARY KEY,
		room_id INTEGER NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
		ts      INTEGER NOT NULL,
		degree  REAL NOT NULL CHECK (degree >= 0)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_samples_room_ts ON samples(room_id, ts)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_samples_room_ts_degree ON samples(room_id, ts, degree)`,
	`CREATE TABLE IF NOT EXISTS archives (
		id          INTEGER PRIMARY KEY,
		room_id     INTEGER NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
		name        TEXT NOT NULL,
		start_ts    INTEGER NOT NULL,
		end_ts      INTEGER NOT NULL,
		records_num INTEGER NOT NULL CHECK (records_num > 0),
		created_at  INTEGER NOT NULL,
		deleted_at  INTEGER,
		UNIQUE (room_id, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_archives_room_created ON archives(room_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_archives_room_start ON archives(room_id, start_ts)`,
	`CREATE TABLE IF NOT EXISTS archive_samples (
		id         INTEGER PRIMARY KEY,
		archive_id INTEGER NOT NULL REFERENCES archives(id) ON DELETE CASCADE,
		ts         INTEGER NOT NULL,
		degree     REAL NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_archive_samples_archive_ts ON archive_samples(archive_id, ts)`,
	`CREATE TABLE IF NOT EXISTS tokens (
		id           INTEGER PRIMARY KEY,
		name         TEXT NOT NULL UNIQUE,
		token_prefix TEXT NOT NULL,
		token_sha256 TEXT NOT NULL UNIQUE,
		scopes       TEXT NOT NULL,
		created_at   INTEGER NOT NULL,
		last_used_at INTEGER,
		revoked_at   INTEGER
	)`,
	`CREATE TABLE IF NOT EXISTS session (
		id              INTEGER PRIMARY KEY CHECK (id = 1),
		jsession_id     TEXT NOT NULL,
		cookie          TEXT NOT NULL,
		csrf_token      TEXT NOT NULL,
		updated_at      INTEGER NOT NULL,
		last_checked_at INTEGER,
		state           TEXT NOT NULL DEFAULT 'unknown'
	)`,
	`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
}
