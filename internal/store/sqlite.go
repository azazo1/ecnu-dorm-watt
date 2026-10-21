package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	// defaultBusyTimeout 是未指定忙等待时间时的默认值.
	defaultBusyTimeout = 5 * time.Second
	// databaseFilePerm 是数据库文件的目标权限.
	databaseFilePerm os.FileMode = 0o600
	// dataDirPerm 是数据库父目录的目标权限.
	dataDirPerm os.FileMode = 0o700
	// activeRoomKey 是 settings 中保存活动房间 id 的键.
	activeRoomKey = "active_room_id"
)

// rowScanner 抽象 sql.Row 与 sql.Rows 的扫描接口.
type rowScanner interface {
	Scan(dest ...any) error
}

// sqliteDB 是基于 SQLite 的 DB 实现.
// 写连接池固定为 1 个连接, 读连接池允许多个连接.
type sqliteDB struct {
	write *sql.DB
	read  *sql.DB
	path  string
	// memory 表示内存数据库, 此时读写共用一个连接池.
	memory bool

	closeOnce sync.Once
	closeErr  error
}

var _ DB = (*sqliteDB)(nil)

// openSQLite 打开或创建 SQLite 数据库并完成迁移.
func openSQLite(ctx context.Context, opts Options) (DB, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, errors.New("数据库路径不能为空")
	}
	memory := isMemoryPath(path)
	if !memory {
		if err := prepareDataDir(filepath.Dir(path)); err != nil {
			return nil, err
		}
	}

	write, err := sql.Open("sqlite", sqliteDSN(path, opts.BusyTimeout, true))
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	if err := write.PingContext(ctx); err != nil {
		write.Close()
		return nil, fmt.Errorf("连接数据库 %s 失败: %w", path, err)
	}
	if !memory {
		chmodDatabaseFiles(path)
	}
	if err := quickCheck(ctx, write); err != nil {
		write.Close()
		return nil, err
	}
	if err := migrate(ctx, write); err != nil {
		write.Close()
		return nil, err
	}

	db := &sqliteDB{write: write, read: write, path: path, memory: memory}
	if memory {
		return db, nil
	}

	read, err := sql.Open("sqlite", sqliteDSN(path, opts.BusyTimeout, false))
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	read.SetMaxIdleConns(4)
	if err := read.PingContext(ctx); err != nil {
		read.Close()
		write.Close()
		return nil, fmt.Errorf("连接数据库 %s 失败: %w", path, err)
	}
	db.read = read
	chmodDatabaseFiles(path)
	return db, nil
}

// sqliteDSN 构造带连接参数的 DSN.
// immediateTx 为 true 时使用 BEGIN IMMEDIATE 开启事务, 供写连接使用.
func sqliteDSN(path string, busy time.Duration, immediateTx bool) string {
	ms := busy.Milliseconds()
	if ms <= 0 {
		ms = defaultBusyTimeout.Milliseconds()
	}
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", ms))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if immediateTx {
		q.Set("_txlock", "immediate")
	}
	return "file:" + escapeDSNPath(path) + "?" + q.Encode()
}

// escapeDSNPath 转义会破坏 DSN 结构的路径字符.
func escapeDSNPath(path string) string {
	if os.PathSeparator == '\\' {
		path = filepath.ToSlash(path)
	}
	return strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
}

// isMemoryPath 判断路径是否指向内存数据库.
func isMemoryPath(path string) bool {
	return path == ":memory:" || strings.Contains(path, "mode=memory")
}

// prepareDataDir 确保数据库父目录存在且权限为 0700.
func prepareDataDir(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("数据目录 %s 已存在但不是目录", dir)
		}
		return nil
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, dataDirPerm); err != nil {
			return fmt.Errorf("创建数据目录 %s 失败: %w", dir, err)
		}
		// 消除 umask 对权限的影响.
		_ = os.Chmod(dir, dataDirPerm)
		return nil
	default:
		return fmt.Errorf("检查数据目录 %s 失败: %w", dir, err)
	}
}

// chmodDatabaseFiles 尽力把数据库文件及其附属文件权限收紧到 0600.
// Windows 的权限模型不同, 失败时不阻止启动.
func chmodDatabaseFiles(path string) {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(p, databaseFilePerm)
	}
}

// quickCheck 执行启动时的完整性检查, 失败则拒绝继续使用该数据库.
func quickCheck(ctx context.Context, db *sql.DB) error {
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("数据库完整性检查失败: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("数据库完整性检查失败: %s, 请从备份恢复", result)
	}
	return nil
}

// Close 在关闭前做 WAL 检查点, 然后关闭读写连接池.
func (d *sqliteDB) Close() error {
	d.closeOnce.Do(func() {
		var firstErr error
		if d.read != d.write {
			if err := d.read.Close(); err != nil {
				firstErr = err
			}
		}
		if !d.memory {
			if _, err := d.write.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("关闭前执行 WAL 检查点失败: %w", err)
			}
		}
		if err := d.write.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		d.closeErr = firstErr
	})
	return d.closeErr
}

// Backup 使用 VACUUM INTO 生成一致性快照, 目标文件必须不存在.
func (d *sqliteDB) Backup(ctx context.Context, out string) error {
	if strings.TrimSpace(out) == "" {
		return errors.New("备份路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(out), dataDirPerm); err != nil {
		return fmt.Errorf("创建备份目录失败: %w", err)
	}
	if _, err := os.Stat(out); err == nil {
		return fmt.Errorf("%w: 备份文件已存在: %s", ErrConflict, out)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := d.write.ExecContext(ctx, "VACUUM INTO ?", out); err != nil {
		return fmt.Errorf("备份数据库失败: %w", err)
	}
	if err := os.Chmod(out, databaseFilePerm); err != nil {
		return fmt.Errorf("设置备份文件权限失败: %w", err)
	}
	return nil
}

// storageError 把驱动错误转换为存储层语义错误.
func storageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return ErrDuplicate
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
			// 引用不存在的行, 对调用方而言等价于目标不存在.
			return ErrNotFound
		}
	}
	return err
}

// toMillis 把时间转换为 UTC Unix 毫秒, 数据库只保存这一种时间格式.
func toMillis(t time.Time) int64 {
	return t.UTC().UnixMilli()
}

// fromMillis 把 UTC Unix 毫秒还原为 UTC 时间.
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// nullableMillis 把可选时间转换为可写入数据库的值.
func nullableMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return toMillis(*t)
}

// nullableTime 把可空毫秒列转换为时间指针.
func nullableTime(ns sql.NullInt64) *time.Time {
	if !ns.Valid {
		return nil
	}
	t := fromMillis(ns.Int64)
	return &t
}

// roundDegree 把电量四舍五入到两位小数.
func roundDegree(degree float64) float64 {
	return math.Round(degree*100) / 100
}

// encodeScopes 把权限范围编码为逗号分隔字符串.
func encodeScopes(scopes []string) string {
	cleaned := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" || strings.Contains(s, ",") {
			continue
		}
		cleaned = append(cleaned, s)
	}
	return strings.Join(cleaned, ",")
}

// decodeScopes 解析逗号分隔的权限范围.
func decodeScopes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		scopes = append(scopes, part)
	}
	if len(scopes) == 0 {
		return nil
	}
	return scopes
}

// scanRoom 从查询结果读取房间.
func scanRoom(sc rowScanner) (Room, error) {
	var (
		room    Room
		created int64
	)
	if err := sc.Scan(&room.ID, &room.RoomNo, &room.Elcarea, &room.Elcbuis, &created); err != nil {
		return Room{}, err
	}
	room.CreatedAt = fromMillis(created)
	return room, nil
}

// scanToken 从查询结果读取 token.
func scanToken(sc rowScanner) (Token, error) {
	var (
		tok      Token
		scopes   string
		created  int64
		lastUsed sql.NullInt64
		revoked  sql.NullInt64
	)
	if err := sc.Scan(&tok.ID, &tok.Name, &tok.Prefix, &tok.Hash, &scopes, &created, &lastUsed, &revoked); err != nil {
		return Token{}, err
	}
	tok.Scopes = decodeScopes(scopes)
	tok.CreatedAt = fromMillis(created)
	tok.LastUsedAt = nullableTime(lastUsed)
	tok.RevokedAt = nullableTime(revoked)
	return tok, nil
}

const tokenColumns = "id, name, token_prefix, token_sha256, scopes, created_at, last_used_at, revoked_at"

// CreateToken 创建 token, name 或摘要重复时返回 ErrDuplicate.
func (d *sqliteDB) CreateToken(ctx context.Context, name, prefix, hash string, scopes []string, now time.Time) (Token, error) {
	ms := toMillis(now)
	encoded := encodeScopes(scopes)
	res, err := d.write.ExecContext(ctx,
		`INSERT INTO tokens(name, token_prefix, token_sha256, scopes, created_at) VALUES(?, ?, ?, ?, ?)`,
		name, prefix, hash, encoded, ms)
	if err != nil {
		return Token{}, storageError(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Token{}, err
	}
	return Token{
		ID:        id,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		Scopes:    decodeScopes(encoded),
		CreatedAt: fromMillis(ms),
	}, nil
}

// ListTokens 按创建顺序列出全部 token, 包含已撤销记录.
func (d *sqliteDB) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := d.read.QueryContext(ctx, "SELECT "+tokenColumns+" FROM tokens ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]Token, 0, 4)
	for rows.Next() {
		tok, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
	}
	return tokens, rows.Err()
}

// RevokeToken 标记 token 为已撤销, 重复撤销视为成功.
func (d *sqliteDB) RevokeToken(ctx context.Context, name string, now time.Time) error {
	res, err := d.write.ExecContext(ctx,
		`UPDATE tokens SET revoked_at = ? WHERE name = ? AND revoked_at IS NULL`,
		toMillis(now), name)
	if err != nil {
		return storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// 没有更新行时区分 token 不存在与已经撤销.
	var id int64
	err = d.write.QueryRowContext(ctx, `SELECT id FROM tokens WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return nil
}

// RotateToken 在一个事务内替换指定名称 token 的密钥, 旧摘要立即失效.
// name 在 tokens 表上唯一, 因此轮换在原记录上更新摘要并清除使用与撤销时间,
// 而不是插入一条同名记录.
func (d *sqliteDB) RotateToken(ctx context.Context, name, prefix, hash string, scopes []string, now time.Time) (Token, error) {
	encoded := encodeScopes(scopes)
	ms := toMillis(now)
	tx, err := d.write.BeginTx(ctx, nil)
	if err != nil {
		return Token{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM tokens WHERE name = ?`, name).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Token{}, ErrNotFound
		}
		return Token{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tokens SET token_prefix = ?, token_sha256 = ?, scopes = ?, created_at = ?,
			last_used_at = NULL, revoked_at = NULL WHERE id = ?`,
		prefix, hash, encoded, ms, id); err != nil {
		return Token{}, storageError(err)
	}
	if err := tx.Commit(); err != nil {
		return Token{}, err
	}
	return Token{
		ID:        id,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		Scopes:    decodeScopes(encoded),
		CreatedAt: fromMillis(ms),
	}, nil
}

// FindTokenByHash 按摘要查找 token. 摘要不存在返回 ErrNotFound,
// 已撤销返回 ErrTokenRevoked.
func (d *sqliteDB) FindTokenByHash(ctx context.Context, hash string) (*Token, error) {
	row := d.read.QueryRowContext(ctx, "SELECT "+tokenColumns+" FROM tokens WHERE token_sha256 = ?", hash)
	tok, err := scanToken(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if tok.RevokedAt != nil {
		return nil, ErrTokenRevoked
	}
	return &tok, nil
}

// TouchToken 更新 token 的最后使用时间.
func (d *sqliteDB) TouchToken(ctx context.Context, id int64, now time.Time) error {
	res, err := d.write.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE id = ?`, toMillis(now), id)
	if err != nil {
		return storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var found int64
	err = d.write.QueryRowContext(ctx, `SELECT id FROM tokens WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return nil
}

// EnsureRoom 返回已存在的房间, 不存在时创建.
// 第二个返回值表示本次是否新建.
func (d *sqliteDB) EnsureRoom(ctx context.Context, roomNo string, elcarea int, elcbuis string, now time.Time) (Room, bool, error) {
	ms := toMillis(now)
	res, err := d.write.ExecContext(ctx,
		`INSERT INTO rooms(room_no, elcarea, elcbuis, created_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(room_no, elcarea, elcbuis) DO NOTHING`,
		roomNo, elcarea, elcbuis, ms)
	if err != nil {
		return Room{}, false, storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Room{}, false, err
	}
	if n > 0 {
		id, err := res.LastInsertId()
		if err != nil {
			return Room{}, false, err
		}
		return Room{ID: id, RoomNo: roomNo, Elcarea: elcarea, Elcbuis: elcbuis, CreatedAt: fromMillis(ms)}, true, nil
	}

	room, err := scanRoom(d.write.QueryRowContext(ctx,
		`SELECT id, room_no, elcarea, elcbuis, created_at FROM rooms
		WHERE room_no = ? AND elcarea = ? AND elcbuis = ?`, roomNo, elcarea, elcbuis))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, false, ErrNotFound
		}
		return Room{}, false, err
	}
	return room, false, nil
}

// ListRooms 按创建顺序列出全部房间.
func (d *sqliteDB) ListRooms(ctx context.Context) ([]Room, error) {
	rows, err := d.read.QueryContext(ctx, `SELECT id, room_no, elcarea, elcbuis, created_at FROM rooms ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]Room, 0, 4)
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

// GetRoom 按 id 读取房间, 不存在返回 ErrNotFound.
func (d *sqliteDB) GetRoom(ctx context.Context, id int64) (*Room, error) {
	room, err := scanRoom(d.read.QueryRowContext(ctx,
		`SELECT id, room_no, elcarea, elcbuis, created_at FROM rooms WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

// ActiveRoom 读取 settings 中记录的活动房间.
func (d *sqliteDB) ActiveRoom(ctx context.Context) (*Room, error) {
	var value string
	err := d.read.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, activeRoomKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("活动房间设置值 %q 无效: %w", value, err)
	}
	return d.GetRoom(ctx, id)
}

// SetActiveRoom 设置活动房间, 房间不存在返回 ErrNotFound.
func (d *sqliteDB) SetActiveRoom(ctx context.Context, id int64) error {
	var found int64
	err := d.write.QueryRowContext(ctx, `SELECT id FROM rooms WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = d.write.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		activeRoomKey, strconv.FormatInt(id, 10))
	return storageError(err)
}

// ClearActiveRoom 删除活动房间设置.
func (d *sqliteDB) ClearActiveRoom(ctx context.Context) error {
	_, err := d.write.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, activeRoomKey)
	return storageError(err)
}

// RoomStats 统计房间的样本数与最近样本时间.
func (d *sqliteDB) RoomStats(ctx context.Context, id int64) (RoomStats, error) {
	var (
		stats RoomStats
		last  sql.NullInt64
	)
	if err := d.read.QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(ts) FROM samples WHERE room_id = ?`, id).Scan(&stats.SampleCount, &last); err != nil {
		return RoomStats{}, err
	}
	stats.LastSampleAt = nullableTime(last)
	return stats, nil
}

// InsertSample 插入样本, 同房间同时间同电量视为已存在并返回 false.
func (d *sqliteDB) InsertSample(ctx context.Context, roomID int64, t time.Time, degree float64) (bool, error) {
	res, err := d.write.ExecContext(ctx,
		`INSERT INTO samples(room_id, ts, degree) VALUES(?, ?, ?)
		ON CONFLICT(room_id, ts, degree) DO NOTHING`,
		roomID, toMillis(t), roundDegree(degree))
	if err != nil {
		return false, storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// LastSample 返回房间时间最晚的样本, 没有样本返回 ErrNotFound.
func (d *sqliteDB) LastSample(ctx context.Context, roomID int64) (*Sample, error) {
	var (
		ms     int64
		degree float64
	)
	err := d.read.QueryRowContext(ctx,
		`SELECT ts, degree FROM samples WHERE room_id = ? ORDER BY ts DESC LIMIT 1`, roomID).Scan(&ms, &degree)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &Sample{Time: fromMillis(ms), Degree: degree}, nil
}

// sampleFilter 生成 samples 的范围条件与参数, 时间范围两端均为闭区间.
func sampleFilter(roomID int64, from, to *time.Time) (string, []any) {
	where := "room_id = ?"
	args := []any{roomID}
	if from != nil {
		where += " AND ts >= ?"
		args = append(args, toMillis(*from))
	}
	if to != nil {
		where += " AND ts <= ?"
		args = append(args, toMillis(*to))
	}
	return where, args
}

// QuerySamples 按时间升序查询样本, limit <= 0 表示不限条数.
func (d *sqliteDB) QuerySamples(ctx context.Context, roomID int64, from, to *time.Time, limit int) ([]Sample, error) {
	where, args := sampleFilter(roomID, from, to)
	query := "SELECT ts, degree FROM samples WHERE " + where + " ORDER BY ts"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := d.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	samples := make([]Sample, 0, 64)
	for rows.Next() {
		var (
			ms     int64
			degree float64
		)
		if err := rows.Scan(&ms, &degree); err != nil {
			return nil, err
		}
		samples = append(samples, Sample{Time: fromMillis(ms), Degree: degree})
	}
	return samples, rows.Err()
}

// CountSamples 统计时间范围内的样本数.
func (d *sqliteDB) CountSamples(ctx context.Context, roomID int64, from, to *time.Time) (int, error) {
	where, args := sampleFilter(roomID, from, to)
	var count int
	if err := d.read.QueryRowContext(ctx, "SELECT COUNT(*) FROM samples WHERE "+where, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// StreamSamples 按时间升序流式读取样本, 回调返回错误时立即停止.
func (d *sqliteDB) StreamSamples(ctx context.Context, roomID int64, from, to *time.Time, fn func(Sample) error) error {
	where, args := sampleFilter(roomID, from, to)
	rows, err := d.read.QueryContext(ctx, "SELECT ts, degree FROM samples WHERE "+where+" ORDER BY ts", args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ms     int64
			degree float64
		)
		if err := rows.Scan(&ms, &degree); err != nil {
			return err
		}
		if err := fn(Sample{Time: fromMillis(ms), Degree: degree}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// SaveSession 覆盖保存唯一的一行会话记录.
func (d *sqliteDB) SaveSession(ctx context.Context, s Session) error {
	state := strings.TrimSpace(s.State)
	if state == "" {
		state = "unknown"
	}
	_, err := d.write.ExecContext(ctx,
		`INSERT INTO session(id, jsession_id, cookie, csrf_token, updated_at, last_checked_at, state)
		VALUES(1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			jsession_id = excluded.jsession_id,
			cookie = excluded.cookie,
			csrf_token = excluded.csrf_token,
			updated_at = excluded.updated_at,
			last_checked_at = excluded.last_checked_at,
			state = excluded.state`,
		s.JSESSIONID, s.Cookie, s.CSRFToken, toMillis(s.UpdatedAt), nullableMillis(s.LastCheckedAt), state)
	return storageError(err)
}

// GetSession 读取会话, 没有记录返回 ErrNotFound.
func (d *sqliteDB) GetSession(ctx context.Context) (*Session, error) {
	var (
		s       Session
		updated int64
		checked sql.NullInt64
	)
	err := d.read.QueryRowContext(ctx,
		`SELECT jsession_id, cookie, csrf_token, state, updated_at, last_checked_at FROM session WHERE id = 1`).
		Scan(&s.JSESSIONID, &s.Cookie, &s.CSRFToken, &s.State, &updated, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.UpdatedAt = fromMillis(updated)
	s.LastCheckedAt = nullableTime(checked)
	return &s, nil
}

// DeleteSession 删除会话记录, 没有记录时也返回成功.
func (d *sqliteDB) DeleteSession(ctx context.Context) error {
	_, err := d.write.ExecContext(ctx, `DELETE FROM session WHERE id = 1`)
	return storageError(err)
}

// SetSessionState 更新会话状态与最近校验时间.
func (d *sqliteDB) SetSessionState(ctx context.Context, state string, checked time.Time) error {
	res, err := d.write.ExecContext(ctx,
		`UPDATE session SET state = ?, last_checked_at = ? WHERE id = 1`, state, toMillis(checked))
	if err != nil {
		return storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanArchive 从查询结果读取归档元数据.
func scanArchive(sc rowScanner) (ArchiveMeta, error) {
	var (
		meta    ArchiveMeta
		start   int64
		end     int64
		created int64
		deleted sql.NullInt64
	)
	if err := sc.Scan(&meta.ID, &meta.RoomID, &meta.Name, &start, &end, &meta.RecordsNum, &created, &deleted); err != nil {
		return ArchiveMeta{}, err
	}
	meta.StartTime = fromMillis(start)
	meta.EndTime = fromMillis(end)
	meta.CreatedAt = fromMillis(created)
	meta.DeletedAt = nullableTime(deleted)
	return meta, nil
}

const archiveColumns = "id, room_id, name, start_ts, end_ts, records_num, created_at, deleted_at"

// CreateArchive 在一个事务内把指定范围内的样本移入归档.
// 范围内没有样本返回 ErrArchiveEmpty, 归档名称冲突返回 ErrDuplicate.
func (d *sqliteDB) CreateArchive(ctx context.Context, roomID int64, name string, from, to *time.Time, now time.Time) (ArchiveMeta, error) {
	tx, err := d.write.BeginTx(ctx, nil)
	if err != nil {
		return ArchiveMeta{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var found int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM rooms WHERE id = ?`, roomID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArchiveMeta{}, ErrNotFound
		}
		return ArchiveMeta{}, err
	}

	// 名称唯一性包含已软删除的归档, V1 不允许复用归档名.
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM archives WHERE room_id = ? AND name = ?`, roomID, name).Scan(&existing)
	switch {
	case err == nil:
		return ArchiveMeta{}, ErrDuplicate
	case !errors.Is(err, sql.ErrNoRows):
		return ArchiveMeta{}, err
	}

	where, args := sampleFilter(roomID, from, to)
	var (
		start  sql.NullInt64
		end    sql.NullInt64
		amount int
	)
	if err := tx.QueryRowContext(ctx, "SELECT MIN(ts), MAX(ts), COUNT(*) FROM samples WHERE "+where, args...).
		Scan(&start, &end, &amount); err != nil {
		return ArchiveMeta{}, err
	}
	if !start.Valid || !end.Valid || amount == 0 {
		return ArchiveMeta{}, ErrArchiveEmpty
	}

	ms := toMillis(now)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO archives(room_id, name, start_ts, end_ts, records_num, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		roomID, name, start.Int64, end.Int64, amount, ms)
	if err != nil {
		return ArchiveMeta{}, storageError(err)
	}
	archiveID, err := res.LastInsertId()
	if err != nil {
		return ArchiveMeta{}, err
	}

	insert, err := tx.ExecContext(ctx,
		`INSERT INTO archive_samples(archive_id, ts, degree)
		SELECT ?, ts, degree FROM samples WHERE room_id = ? AND ts >= ? AND ts <= ?`,
		archiveID, roomID, start.Int64, end.Int64)
	if err != nil {
		return ArchiveMeta{}, err
	}
	records, err := insert.RowsAffected()
	if err != nil {
		return ArchiveMeta{}, err
	}
	if records == 0 {
		// 理论上不可达, 范围非空时插入必然有行.
		return ArchiveMeta{}, ErrArchiveEmpty
	}
	if records != int64(amount) {
		return ArchiveMeta{}, fmt.Errorf("归档样本数量不一致: 预期 %d, 实际 %d", amount, records)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM samples WHERE room_id = ? AND ts >= ? AND ts <= ?`,
		roomID, start.Int64, end.Int64); err != nil {
		return ArchiveMeta{}, err
	}

	if err := tx.Commit(); err != nil {
		return ArchiveMeta{}, err
	}

	return ArchiveMeta{
		ID:         archiveID,
		RoomID:     roomID,
		Name:       name,
		StartTime:  fromMillis(start.Int64),
		EndTime:    fromMillis(end.Int64),
		RecordsNum: int(records),
		CreatedAt:  fromMillis(ms),
	}, nil
}

// ListArchives 列出归档, roomID 为 nil 表示所有房间.
// 默认不包含已软删除的归档, 按开始时间, 房间 id, 名称升序.
func (d *sqliteDB) ListArchives(ctx context.Context, roomID *int64, includeDeleted bool) ([]ArchiveMeta, error) {
	query := "SELECT " + archiveColumns + " FROM archives WHERE 1 = 1"
	args := make([]any, 0, 2)
	if roomID != nil {
		query += " AND room_id = ?"
		args = append(args, *roomID)
	}
	if !includeDeleted {
		query += " AND deleted_at IS NULL"
	}
	query += " ORDER BY start_ts, room_id, name"

	rows, err := d.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	archives := make([]ArchiveMeta, 0, 8)
	for rows.Next() {
		meta, err := scanArchive(rows)
		if err != nil {
			return nil, err
		}
		archives = append(archives, meta)
	}
	return archives, rows.Err()
}

// GetArchive 按名称读取未删除的归档, roomID 为 nil 表示任意房间.
func (d *sqliteDB) GetArchive(ctx context.Context, roomID *int64, name string) (*ArchiveMeta, error) {
	query := "SELECT " + archiveColumns + " FROM archives WHERE name = ? AND deleted_at IS NULL"
	args := []any{name}
	if roomID != nil {
		query += " AND room_id = ?"
		args = append(args, *roomID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT 1"

	meta, err := scanArchive(d.read.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &meta, nil
}

// StreamArchive 按时间升序流式读取归档内样本.
func (d *sqliteDB) StreamArchive(ctx context.Context, archiveID int64, fn func(Sample) error) error {
	rows, err := d.read.QueryContext(ctx,
		`SELECT ts, degree FROM archive_samples WHERE archive_id = ? ORDER BY ts`, archiveID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ms     int64
			degree float64
		)
		if err := rows.Scan(&ms, &degree); err != nil {
			return err
		}
		if err := fn(Sample{Time: fromMillis(ms), Degree: degree}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DeleteArchive 软删除归档, 目标不存在或已删除返回 ErrNotFound.
func (d *sqliteDB) DeleteArchive(ctx context.Context, roomID *int64, name string, now time.Time) error {
	query := `UPDATE archives SET deleted_at = ? WHERE name = ? AND deleted_at IS NULL`
	args := []any{toMillis(now), name}
	if roomID != nil {
		query += " AND room_id = ?"
		args = append(args, *roomID)
	}
	res, err := d.write.ExecContext(ctx, query, args...)
	if err != nil {
		return storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeDeleted 物理删除删除时间早于 before 的归档, 归档样本由外键级联删除.
func (d *sqliteDB) PurgeDeleted(ctx context.Context, before time.Time) (int, error) {
	res, err := d.write.ExecContext(ctx,
		`DELETE FROM archives WHERE deleted_at IS NOT NULL AND deleted_at < ?`, toMillis(before))
	if err != nil {
		return 0, storageError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// Stats 返回样本总数与未删除归档数.
func (d *sqliteDB) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	err := d.read.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM samples), (SELECT COUNT(*) FROM archives WHERE deleted_at IS NULL)`).
		Scan(&stats.Samples, &stats.Archives)
	if err != nil {
		return Stats{}, err
	}
	return stats, nil
}
