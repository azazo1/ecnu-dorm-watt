package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// newTestDB 在临时目录创建一个数据库并返回它.
func newTestDB(t *testing.T) (DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "dormwatt.db")
	db, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("关闭数据库失败: %v", err)
		}
	})
	return db, path
}

// openRaw 直接用驱动打开数据库, 用于检查存储层之外的数据库状态.
func openRaw(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("打开原始数据库连接失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// baseTime 返回测试基准时间, 带非 UTC 时区以便检查归一化.
func baseTime() time.Time {
	zone := time.FixedZone("CST", 8*60*60)
	return time.Date(2026, 1, 24, 20, 0, 0, 0, zone)
}

// approx 比较两个浮点电量.
func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func mustRoom(t *testing.T, db DB, now time.Time) Room {
	t.Helper()
	room, created, err := db.EnsureRoom(context.Background(), "4408_MH_83_257", 83, "257", now)
	if err != nil {
		t.Fatalf("创建房间失败: %v", err)
	}
	if !created {
		t.Fatal("首次创建房间应返回新建标记")
	}
	return room
}

func mustInsertSamples(t *testing.T, db DB, roomID int64, times []time.Time, degrees []float64) {
	t.Helper()
	for i := range times {
		ok, err := db.InsertSample(context.Background(), roomID, times[i], degrees[i])
		if err != nil {
			t.Fatalf("插入样本失败: %v", err)
		}
		if !ok {
			t.Fatalf("第 %d 条样本应成功插入", i)
		}
	}
}

func TestOpenAndMigrate(t *testing.T) {
	db, path := newTestDB(t)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取数据库文件信息失败: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("数据库文件权限为 %o, 期望 600", perm)
		}
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("读取数据目录信息失败: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("数据目录权限为 %o, 期望 700", perm)
		}
	}

	// 用同样的 DSN 打开旁路连接, 检查连接参数已生效.
	raw := openRaw(t, sqliteDSN(path, 0, false))
	var (
		version   int
		foreign   int
		busy      int
		syncValue int
		journal   string
	)
	if err := raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取 user_version 失败: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("user_version 为 %d, 期望 %d", version, currentSchemaVersion)
	}
	if err := raw.QueryRow("PRAGMA foreign_keys").Scan(&foreign); err != nil {
		t.Fatalf("读取 foreign_keys 失败: %v", err)
	}
	if foreign != 1 {
		t.Errorf("foreign_keys 为 %d, 期望 1", foreign)
	}
	if err := raw.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("读取 busy_timeout 失败: %v", err)
	}
	if busy != int(defaultBusyTimeout.Milliseconds()) {
		t.Errorf("busy_timeout 为 %d, 期望 %d", busy, defaultBusyTimeout.Milliseconds())
	}
	if err := raw.QueryRow("PRAGMA synchronous").Scan(&syncValue); err != nil {
		t.Fatalf("读取 synchronous 失败: %v", err)
	}
	if syncValue != 1 {
		t.Errorf("synchronous 为 %d, 期望 1 (NORMAL)", syncValue)
	}
	if err := raw.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("读取 journal_mode 失败: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode 为 %q, 期望 wal", journal)
	}

	for _, name := range []string{"meta", "rooms", "samples", "archives", "archive_samples", "tokens", "session", "settings"} {
		var count int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).
			Scan(&count); err != nil {
			t.Fatalf("查询表 %s 失败: %v", name, err)
		}
		if count != 1 {
			t.Errorf("表 %s 不存在", name)
		}
	}

	// 关闭后重新打开, 迁移应幂等且数据保留.
	if err := db.Close(); err != nil {
		t.Fatalf("关闭数据库失败: %v", err)
	}
	reopened, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatalf("重新打开数据库失败: %v", err)
	}
	defer reopened.Close()
	if err := reopened.ClearActiveRoom(context.Background()); err != nil {
		t.Fatalf("重新打开后执行写入失败: %v", err)
	}
	var reopenedVersion int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&reopenedVersion); err != nil {
		t.Fatalf("重新打开后读取 user_version 失败: %v", err)
	}
	if reopenedVersion != currentSchemaVersion {
		t.Errorf("重新打开后 user_version 为 %d, 期望 %d", reopenedVersion, currentSchemaVersion)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(context.Background(), Options{Path: "  "}); err == nil {
		t.Fatal("空路径应返回错误")
	}
}

func TestOpenInMemory(t *testing.T) {
	db, err := Open(context.Background(), Options{Path: ":memory:"})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := baseTime()
	room, created, err := db.EnsureRoom(ctx, "4408_MH_83_257", 83, "257", now)
	if err != nil || !created {
		t.Fatalf("内存数据库创建房间失败: created=%v err=%v", created, err)
	}
	if _, err := db.InsertSample(ctx, room.ID, now, 12.3); err != nil {
		t.Fatalf("内存数据库插入样本失败: %v", err)
	}
	count, err := db.CountSamples(ctx, room.ID, nil, nil)
	if err != nil {
		t.Fatalf("内存数据库统计样本失败: %v", err)
	}
	if count != 1 {
		t.Errorf("内存数据库样本数为 %d, 期望 1", count)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	raw := openRaw(t, "file:"+path)
	if _, err := raw.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("写入 user_version 失败: %v", err)
	}
	_ = raw.Close()

	if _, err := Open(context.Background(), Options{Path: path}); err == nil {
		t.Fatal("数据库版本高于程序支持的版本时应拒绝打开")
	}
}

func TestMigrationRollback(t *testing.T) {
	_, path := newTestDB(t)
	raw := openRaw(t, sqliteDSN(path, 0, false))
	failing := migration{
		version: currentSchemaVersion + 1,
		name:    "failing",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `CREATE TABLE should_rollback (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			return errors.New("模拟迁移失败")
		},
	}
	if err := runMigration(context.Background(), raw, failing); err == nil {
		t.Fatal("迁移失败时应返回错误")
	}

	var version int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取 user_version 失败: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("失败迁移后 user_version 为 %d, 期望 %d", version, currentSchemaVersion)
	}
	var created int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'should_rollback'`).
		Scan(&created); err != nil {
		t.Fatalf("查询回滚表失败: %v", err)
	}
	if created != 0 {
		t.Error("失败迁移创建的表应被回滚")
	}
}

func TestRoomsAndActiveRoom(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	now := baseTime()

	room, created, err := db.EnsureRoom(ctx, "4408_MH_83_257", 83, "257", now)
	if err != nil {
		t.Fatalf("创建房间失败: %v", err)
	}
	if !created || room.ID == 0 {
		t.Fatalf("首次创建房间结果异常: created=%v id=%d", created, room.ID)
	}
	if !room.CreatedAt.Equal(now) || room.CreatedAt.Location() != time.UTC {
		t.Errorf("房间创建时间为 %v, 期望 UTC 的 %v", room.CreatedAt, now)
	}

	same, created, err := db.EnsureRoom(ctx, "4408_MH_83_257", 83, "257", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("再次创建同名房间失败: %v", err)
	}
	if created {
		t.Error("已存在的房间不应返回新建标记")
	}
	if same.ID != room.ID {
		t.Errorf("重复创建返回 id %d, 期望 %d", same.ID, room.ID)
	}
	if !same.CreatedAt.Equal(now) {
		t.Errorf("重复创建改动了创建时间: %v", same.CreatedAt)
	}

	other, created, err := db.EnsureRoom(ctx, "4408_MH_83_257", 84, "257", now)
	if err != nil {
		t.Fatalf("创建不同 area 的房间失败: %v", err)
	}
	if !created || other.ID == room.ID {
		t.Errorf("不同 elcarea 应视为新房间: created=%v id=%d", created, other.ID)
	}

	rooms, err := db.ListRooms(ctx)
	if err != nil {
		t.Fatalf("列出房间失败: %v", err)
	}
	if len(rooms) != 2 {
		t.Errorf("房间数为 %d, 期望 2", len(rooms))
	}

	got, err := db.GetRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("读取房间失败: %v", err)
	}
	if got.RoomNo != room.RoomNo || got.Elcarea != 83 || got.Elcbuis != "257" {
		t.Errorf("读取的房间内容不符: %+v", got)
	}
	if _, err := db.GetRoom(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("读取不存在的房间返回 %v, 期望 ErrNotFound", err)
	}

	if err := db.SetActiveRoom(ctx, room.ID); err != nil {
		t.Fatalf("设置活动房间失败: %v", err)
	}
	active, err := db.ActiveRoom(ctx)
	if err != nil {
		t.Fatalf("读取活动房间失败: %v", err)
	}
	if active.ID != room.ID {
		t.Errorf("活动房间为 %d, 期望 %d", active.ID, room.ID)
	}

	if err := db.ClearActiveRoom(ctx); err != nil {
		t.Fatalf("清除活动房间失败: %v", err)
	}
	if _, err := db.ActiveRoom(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("没有活动房间时返回 %v, 期望 ErrNotFound", err)
	}

	if err := db.SetActiveRoom(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("设置不存在的房间返回 %v, 期望 ErrNotFound", err)
	}
}

func TestSamplesDedupAndRange(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	room := mustRoom(t, db, baseTime())

	t0 := baseTime()
	t1 := t0.Add(time.Minute)
	t2 := t0.Add(2 * time.Minute)
	t3 := t0.Add(3 * time.Minute)

	ok, err := db.InsertSample(ctx, room.ID, t0, 10.004)
	if err != nil || !ok {
		t.Fatalf("插入样本失败: ok=%v err=%v", ok, err)
	}
	ok, err = db.InsertSample(ctx, room.ID, t0, 10.004)
	if err != nil {
		t.Fatalf("重复插入样本失败: %v", err)
	}
	if ok {
		t.Error("重复样本应返回 false")
	}
	ok, err = db.InsertSample(ctx, room.ID, t0, 20)
	if err != nil || !ok {
		t.Fatalf("同时间不同电量应允许插入: ok=%v err=%v", ok, err)
	}
	ok, err = db.InsertSample(ctx, room.ID, t1, 10.126)
	if err != nil || !ok {
		t.Fatalf("插入样本失败: ok=%v err=%v", ok, err)
	}
	// 四舍五入后相同的电量视为重复.
	ok, err = db.InsertSample(ctx, room.ID, t1, 10.126)
	if err != nil {
		t.Fatalf("重复插入样本失败: %v", err)
	}
	if ok {
		t.Error("四舍五入后相同的样本应返回 false")
	}
	mustInsertSamples(t, db, room.ID, []time.Time{t2, t3}, []float64{33, 40.5})

	samples, err := db.QuerySamples(ctx, room.ID, nil, nil, 0)
	if err != nil {
		t.Fatalf("查询样本失败: %v", err)
	}
	if len(samples) != 5 {
		t.Fatalf("样本数为 %d, 期望 5", len(samples))
	}
	for i := 1; i < len(samples); i++ {
		if samples[i].Time.Before(samples[i-1].Time) {
			t.Errorf("样本未按时间升序: %v", samples)
			break
		}
	}
	var roundedt0 *Sample
	for i := range samples {
		if samples[i].Time.Equal(t0) && approx(samples[i].Degree, 10) {
			roundedt0 = &samples[i]
			break
		}
	}
	if roundedt0 == nil {
		t.Fatalf("未找到四舍五入到 10 的样本: %v", samples)
	}
	if roundedt0.Time.Location() != time.UTC {
		t.Errorf("样本时间未归一化为 UTC: %v", roundedt0.Time)
	}

	// 闭区间, 两端都包含.
	ranged, err := db.QuerySamples(ctx, room.ID, &t1, &t2, 0)
	if err != nil {
		t.Fatalf("范围查询失败: %v", err)
	}
	if len(ranged) != 2 {
		t.Fatalf("范围查询返回 %d 条, 期望 2", len(ranged))
	}
	if !ranged[0].Time.Equal(t1) || !ranged[1].Time.Equal(t2) {
		t.Errorf("范围查询边界不正确: %v", ranged)
	}
	if !approx(ranged[0].Degree, 10.13) {
		t.Errorf("电量 %v 未四舍五入到 10.13", ranged[0].Degree)
	}

	limited, err := db.QuerySamples(ctx, room.ID, &t1, &t2, 1)
	if err != nil {
		t.Fatalf("带 limit 的查询失败: %v", err)
	}
	if len(limited) != 1 || !limited[0].Time.Equal(t1) {
		t.Errorf("limit 查询结果不符: %v", limited)
	}

	count, err := db.CountSamples(ctx, room.ID, &t1, &t2)
	if err != nil {
		t.Fatalf("统计样本失败: %v", err)
	}
	if count != 2 {
		t.Errorf("范围内样本数为 %d, 期望 2", count)
	}

	last, err := db.LastSample(ctx, room.ID)
	if err != nil {
		t.Fatalf("读取最近样本失败: %v", err)
	}
	if !last.Time.Equal(t3) || !approx(last.Degree, 40.5) {
		t.Errorf("最近样本为 %v, 期望 %v", last, t3)
	}

	streamed := 0
	if err := db.StreamSamples(ctx, room.ID, &t1, &t2, func(Sample) error {
		streamed++
		return nil
	}); err != nil {
		t.Fatalf("流式读取样本失败: %v", err)
	}
	if streamed != 2 {
		t.Errorf("流式读取 %d 条, 期望 2", streamed)
	}

	if _, err := db.QuerySamples(ctx, room.ID, &t2, &t1, 0); err != nil {
		t.Fatalf("空范围查询失败: %v", err)
	}
	if _, err := db.InsertSample(ctx, 9999, t0, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("向不存在的房间插入样本返回 %v, 期望 ErrNotFound", err)
	}
	if _, err := db.LastSample(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的房间没有最近样本, 期望 ErrNotFound, 实际 %v", err)
	}
}

func TestCreateArchiveTransaction(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	room := mustRoom(t, db, baseTime())

	t0 := baseTime()
	times := []time.Time{t0, t0.Add(time.Minute), t0.Add(2 * time.Minute), t0.Add(3 * time.Minute), t0.Add(4 * time.Minute)}
	mustInsertSamples(t, db, room.ID, times, []float64{10, 11, 12, 13, 14})

	createdAt := t0.Add(10 * time.Minute)
	meta, err := db.CreateArchive(ctx, room.ID, "2026-01", &times[1], &times[3], createdAt)
	if err != nil {
		t.Fatalf("创建归档失败: %v", err)
	}
	if meta.ID == 0 {
		t.Error("归档 id 应非 0")
	}
	if meta.RecordsNum != 3 {
		t.Errorf("归档条数为 %d, 期望 3", meta.RecordsNum)
	}
	if !meta.StartTime.Equal(times[1]) || !meta.EndTime.Equal(times[3]) {
		t.Errorf("归档起止时间为 %v 到 %v, 期望 %v 到 %v", meta.StartTime, meta.EndTime, times[1], times[3])
	}
	if !meta.CreatedAt.Equal(createdAt) || meta.DeletedAt != nil {
		t.Errorf("归档元数据异常: %+v", meta)
	}

	count, err := db.CountSamples(ctx, room.ID, nil, nil)
	if err != nil {
		t.Fatalf("统计剩余样本失败: %v", err)
	}
	if count != 2 {
		t.Errorf("归档后剩余样本为 %d, 期望 2", count)
	}

	got, err := db.GetArchive(ctx, &room.ID, "2026-01")
	if err != nil {
		t.Fatalf("读取归档失败: %v", err)
	}
	if got.ID != meta.ID || got.RecordsNum != 3 {
		t.Errorf("读取的归档与创建结果不一致: %+v", got)
	}
	if any, err := db.GetArchive(ctx, nil, "2026-01"); err != nil || any.ID != meta.ID {
		t.Errorf("不指定房间读取归档失败: %v", err)
	}

	var archived []Sample
	if err := db.StreamArchive(ctx, meta.ID, func(s Sample) error {
		archived = append(archived, s)
		return nil
	}); err != nil {
		t.Fatalf("读取归档样本失败: %v", err)
	}
	if len(archived) != 3 {
		t.Fatalf("归档样本为 %d 条, 期望 3", len(archived))
	}
	for i, s := range archived {
		if !s.Time.Equal(times[i+1]) || !approx(s.Degree, float64(11+i)) {
			t.Errorf("第 %d 条归档样本为 %v", i, s)
		}
	}

	listed, err := db.ListArchives(ctx, nil, false)
	if err != nil {
		t.Fatalf("列出归档失败: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != meta.ID {
		t.Errorf("归档列表不符: %+v", listed)
	}

	// 空范围返回 ErrArchiveEmpty, 且不产生任何写入.
	if _, err := db.CreateArchive(ctx, room.ID, "empty", &times[1], &times[3], createdAt); !errors.Is(err, ErrArchiveEmpty) {
		t.Errorf("空范围归档返回 %v, 期望 ErrArchiveEmpty", err)
	}
	count, err = db.CountSamples(ctx, room.ID, nil, nil)
	if err != nil {
		t.Fatalf("统计样本失败: %v", err)
	}
	if count != 2 {
		t.Errorf("空范围归档后样本数为 %d, 期望 2", count)
	}

	if _, err := db.CreateArchive(ctx, 9999, "missing", nil, nil, createdAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的房间创建归档返回 %v, 期望 ErrNotFound", err)
	}
	if _, err := db.GetArchive(ctx, &room.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("读取不存在的归档返回 %v, 期望 ErrNotFound", err)
	}
}

func TestArchiveDuplicateName(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	room := mustRoom(t, db, baseTime())

	t0 := baseTime()
	t1 := t0.Add(time.Minute)
	mustInsertSamples(t, db, room.ID, []time.Time{t0, t1}, []float64{10, 11})
	if _, err := db.CreateArchive(ctx, room.ID, "same", nil, nil, t0); err != nil {
		t.Fatalf("创建归档失败: %v", err)
	}

	mustInsertSamples(t, db, room.ID, []time.Time{t1.Add(time.Minute)}, []float64{12})
	if _, err := db.CreateArchive(ctx, room.ID, "same", nil, nil, t0); !errors.Is(err, ErrDuplicate) {
		t.Errorf("重名归档返回 %v, 期望 ErrDuplicate", err)
	}
	count, err := db.CountSamples(ctx, room.ID, nil, nil)
	if err != nil {
		t.Fatalf("统计样本失败: %v", err)
	}
	if count != 1 {
		t.Errorf("重名归档失败后样本数为 %d, 期望 1", count)
	}

	if err := db.DeleteArchive(ctx, &room.ID, "same", t0); err != nil {
		t.Fatalf("软删除归档失败: %v", err)
	}
	if _, err := db.CreateArchive(ctx, room.ID, "same", nil, nil, t0); !errors.Is(err, ErrDuplicate) {
		t.Errorf("已删除归档的名称不应复用, 实际返回 %v", err)
	}
}

func TestArchiveSoftDeleteAndPurge(t *testing.T) {
	db, path := newTestDB(t)
	ctx := context.Background()
	room := mustRoom(t, db, baseTime())

	t0 := baseTime()
	mustInsertSamples(t, db, room.ID, []time.Time{t0, t0.Add(time.Minute)}, []float64{10, 11})
	meta, err := db.CreateArchive(ctx, room.ID, "old", nil, nil, t0)
	if err != nil {
		t.Fatalf("创建归档失败: %v", err)
	}

	if err := db.DeleteArchive(ctx, &room.ID, "old", t0); err != nil {
		t.Fatalf("软删除归档失败: %v", err)
	}
	if _, err := db.GetArchive(ctx, &room.ID, "old"); !errors.Is(err, ErrNotFound) {
		t.Errorf("已删除归档仍可读取: %v", err)
	}
	visible, err := db.ListArchives(ctx, &room.ID, false)
	if err != nil {
		t.Fatalf("列出归档失败: %v", err)
	}
	if len(visible) != 0 {
		t.Errorf("默认列表应排除已删除归档: %+v", visible)
	}
	all, err := db.ListArchives(ctx, nil, true)
	if err != nil {
		t.Fatalf("列出全部归档失败: %v", err)
	}
	if len(all) != 1 || all[0].DeletedAt == nil {
		t.Fatalf("包含删除的列表不符: %+v", all)
	}
	if err := db.DeleteArchive(ctx, &room.ID, "old", t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复软删除返回 %v, 期望 ErrNotFound", err)
	}

	removed, err := db.PurgeDeleted(ctx, t0.Add(-time.Hour))
	if err != nil {
		t.Fatalf("清理归档失败: %v", err)
	}
	if removed != 0 {
		t.Errorf("过早的清理删除了 %d 条, 期望 0", removed)
	}
	removed, err = db.PurgeDeleted(ctx, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("清理归档失败: %v", err)
	}
	if removed != 1 {
		t.Errorf("清理删除了 %d 条, 期望 1", removed)
	}
	all, err = db.ListArchives(ctx, nil, true)
	if err != nil {
		t.Fatalf("列出全部归档失败: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("清理后仍有归档: %+v", all)
	}

	// 外键级联应删除归档样本.
	raw := openRaw(t, sqliteDSN(path, 0, false))
	var remaining int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM archive_samples WHERE archive_id = ?`, meta.ID).Scan(&remaining); err != nil {
		t.Fatalf("统计归档样本失败: %v", err)
	}
	if remaining != 0 {
		t.Errorf("级联删除后仍有 %d 条归档样本", remaining)
	}

	stats, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("读取统计失败: %v", err)
	}
	if stats.Samples != 0 || stats.Archives != 0 {
		t.Errorf("清理后统计为 %+v, 期望均为 0", stats)
	}
}

func TestArchiveStatsAndOrder(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	now := baseTime()
	first := mustRoom(t, db, now)
	second, _, err := db.EnsureRoom(ctx, "4409_MH_83_257", 83, "257", now)
	if err != nil {
		t.Fatalf("创建第二个房间失败: %v", err)
	}

	t0 := now
	mustInsertSamples(t, db, first.ID, []time.Time{t0.Add(time.Minute)}, []float64{10})
	mustInsertSamples(t, db, second.ID, []time.Time{t0}, []float64{20})
	if _, err := db.CreateArchive(ctx, first.ID, "b", nil, nil, now); err != nil {
		t.Fatalf("创建归档失败: %v", err)
	}
	if _, err := db.CreateArchive(ctx, second.ID, "a", nil, nil, now); err != nil {
		t.Fatalf("创建归档失败: %v", err)
	}

	listed, err := db.ListArchives(ctx, nil, false)
	if err != nil {
		t.Fatalf("列出归档失败: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("归档数为 %d, 期望 2", len(listed))
	}
	// 按 start_ts 升序, 第二个房间的归档时间更早.
	if listed[0].Name != "a" || listed[1].Name != "b" {
		t.Errorf("归档排序不符: %+v", listed)
	}

	roomOnly, err := db.ListArchives(ctx, &first.ID, false)
	if err != nil {
		t.Fatalf("按房间列出归档失败: %v", err)
	}
	if len(roomOnly) != 1 || roomOnly[0].RoomID != first.ID {
		t.Errorf("按房间过滤失败: %+v", roomOnly)
	}

	stats, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("读取统计失败: %v", err)
	}
	if stats.Samples != 0 || stats.Archives != 2 {
		t.Errorf("统计为 %+v, 期望 0 个样本 2 个归档", stats)
	}

	roomStats, err := db.RoomStats(ctx, second.ID)
	if err != nil {
		t.Fatalf("读取房间统计失败: %v", err)
	}
	if roomStats.SampleCount != 0 || roomStats.LastSampleAt != nil {
		t.Errorf("归档后房间统计为 %+v, 期望空", roomStats)
	}
	mustInsertSamples(t, db, second.ID, []time.Time{t0.Add(5 * time.Minute)}, []float64{30})
	roomStats, err = db.RoomStats(ctx, second.ID)
	if err != nil {
		t.Fatalf("读取房间统计失败: %v", err)
	}
	if roomStats.SampleCount != 1 || roomStats.LastSampleAt == nil || !roomStats.LastSampleAt.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("房间统计为 %+v", roomStats)
	}
}

func TestTokens(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	now := baseTime()

	tok, err := db.CreateToken(ctx, "alice", "dw_abc", "hash-1", []string{"read", "write"}, now)
	if err != nil {
		t.Fatalf("创建 token 失败: %v", err)
	}
	if tok.ID == 0 || tok.Name != "alice" || tok.Hash != "hash-1" {
		t.Errorf("创建的 token 异常: %+v", tok)
	}
	if len(tok.Scopes) != 2 || tok.Scopes[0] != "read" || tok.Scopes[1] != "write" {
		t.Errorf("token 权限范围为 %v", tok.Scopes)
	}
	if !tok.CreatedAt.Equal(now) || tok.LastUsedAt != nil || tok.RevokedAt != nil {
		t.Errorf("token 时间字段异常: %+v", tok)
	}

	if _, err := db.CreateToken(ctx, "alice", "dw_x", "hash-2", nil, now); !errors.Is(err, ErrDuplicate) {
		t.Errorf("重名 token 返回 %v, 期望 ErrDuplicate", err)
	}
	if _, err := db.CreateToken(ctx, "bob", "dw_y", "hash-1", nil, now); !errors.Is(err, ErrDuplicate) {
		t.Errorf("重复摘要 token 返回 %v, 期望 ErrDuplicate", err)
	}

	list, err := db.ListTokens(ctx)
	if err != nil {
		t.Fatalf("列出 token 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("token 数为 %d, 期望 1", len(list))
	}

	found, err := db.FindTokenByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("按摘要查找 token 失败: %v", err)
	}
	if found.ID != tok.ID || found.Prefix != "dw_abc" {
		t.Errorf("查找到的 token 异常: %+v", found)
	}
	if _, err := db.FindTokenByHash(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("查找不存在的摘要返回 %v, 期望 ErrNotFound", err)
	}

	usedAt := now.Add(time.Hour)
	if err := db.TouchToken(ctx, tok.ID, usedAt); err != nil {
		t.Fatalf("更新 token 使用时间失败: %v", err)
	}
	found, err = db.FindTokenByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("按摘要查找 token 失败: %v", err)
	}
	if found.LastUsedAt == nil || !found.LastUsedAt.Equal(usedAt) {
		t.Errorf("最后使用时间为 %v, 期望 %v", found.LastUsedAt, usedAt)
	}
	if err := db.TouchToken(ctx, 9999, usedAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("更新不存在的 token 返回 %v, 期望 ErrNotFound", err)
	}

	if err := db.RevokeToken(ctx, "alice", usedAt); err != nil {
		t.Fatalf("撤销 token 失败: %v", err)
	}
	if _, err := db.FindTokenByHash(ctx, "hash-1"); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("已撤销 token 返回 %v, 期望 ErrTokenRevoked", err)
	}
	if err := db.RevokeToken(ctx, "alice", usedAt); err != nil {
		t.Errorf("重复撤销应幂等: %v", err)
	}
	if err := db.RevokeToken(ctx, "missing", usedAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("撤销不存在的 token 返回 %v, 期望 ErrNotFound", err)
	}

	rotatedAt := usedAt.Add(time.Hour)
	rotated, err := db.RotateToken(ctx, "alice", "dw_new", "hash-2", []string{"read"}, rotatedAt)
	if err != nil {
		t.Fatalf("轮换 token 失败: %v", err)
	}
	if rotated.Hash != "hash-2" || rotated.RevokedAt != nil || rotated.LastUsedAt != nil {
		t.Errorf("轮换后的 token 异常: %+v", rotated)
	}
	if !rotated.CreatedAt.Equal(rotatedAt) {
		t.Errorf("轮换后的创建时间为 %v, 期望 %v", rotated.CreatedAt, rotatedAt)
	}
	if _, err := db.FindTokenByHash(ctx, "hash-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("旧摘要应失效, 实际返回 %v", err)
	}
	if _, err := db.FindTokenByHash(ctx, "hash-2"); err != nil {
		t.Errorf("新摘要应可用: %v", err)
	}
	list, err = db.ListTokens(ctx)
	if err != nil {
		t.Fatalf("列出 token 失败: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("轮换后 token 数为 %d, 期望 1", len(list))
	}
	if _, err := db.RotateToken(ctx, "missing", "dw_new", "hash-3", nil, rotatedAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("轮换不存在的 token 返回 %v, 期望 ErrNotFound", err)
	}
}

func TestSessionOverwrite(t *testing.T) {
	db, path := newTestDB(t)
	ctx := context.Background()
	now := baseTime()

	if _, err := db.GetSession(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("没有会话时返回 %v, 期望 ErrNotFound", err)
	}

	first := Session{
		JSESSIONID: "js-1",
		Cookie:     "cookie-1",
		CSRFToken:  "csrf-1",
		State:      "valid",
		UpdatedAt:  now,
	}
	if err := db.SaveSession(ctx, first); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}

	checked := now.Add(time.Minute)
	second := Session{
		JSESSIONID:    "js-2",
		Cookie:        "cookie-2",
		CSRFToken:     "csrf-2",
		State:         "valid",
		UpdatedAt:     now.Add(2 * time.Minute),
		LastCheckedAt: &checked,
	}
	if err := db.SaveSession(ctx, second); err != nil {
		t.Fatalf("覆盖保存会话失败: %v", err)
	}

	got, err := db.GetSession(ctx)
	if err != nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	if got.JSESSIONID != second.JSESSIONID || got.Cookie != second.Cookie || got.CSRFToken != second.CSRFToken {
		t.Errorf("会话内容未覆盖: %+v", got)
	}
	if !got.UpdatedAt.Equal(second.UpdatedAt) {
		t.Errorf("会话更新时间为 %v, 期望 %v", got.UpdatedAt, second.UpdatedAt)
	}
	if got.LastCheckedAt == nil || !got.LastCheckedAt.Equal(checked) {
		t.Errorf("会话校验时间为 %v, 期望 %v", got.LastCheckedAt, checked)
	}

	raw := openRaw(t, sqliteDSN(path, 0, false))
	var rows int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM session`).Scan(&rows); err != nil {
		t.Fatalf("统计会话行数失败: %v", err)
	}
	if rows != 1 {
		t.Errorf("会话行数为 %d, 期望 1", rows)
	}

	stateAt := now.Add(3 * time.Minute)
	if err := db.SetSessionState(ctx, "expired", stateAt); err != nil {
		t.Fatalf("更新会话状态失败: %v", err)
	}
	got, err = db.GetSession(ctx)
	if err != nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	if got.State != "expired" || got.LastCheckedAt == nil || !got.LastCheckedAt.Equal(stateAt) {
		t.Errorf("会话状态为 %+v, 期望 expired 和 %v", got, stateAt)
	}

	if err := db.DeleteSession(ctx); err != nil {
		t.Fatalf("删除会话失败: %v", err)
	}
	if _, err := db.GetSession(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后读取会话返回 %v, 期望 ErrNotFound", err)
	}
	if err := db.SetSessionState(ctx, "valid", stateAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("没有会话时更新状态返回 %v, 期望 ErrNotFound", err)
	}
	if err := db.DeleteSession(ctx); err != nil {
		t.Errorf("重复删除会话应幂等: %v", err)
	}
}
