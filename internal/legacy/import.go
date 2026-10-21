// Package legacy 负责导入旧版 Rust 服务端的 CSV 与 TOML 数据.
package legacy

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// Options 是导入参数.
type Options struct {
	DataDir   string
	ConfigDir string
}

// Result 是导入统计.
type Result struct {
	Rooms        int
	Samples      int
	Archives     int
	SkippedRooms int
	SkippedFiles int
}

type oldRoomConfig struct {
	RoomNo  string `toml:"roomNo"`
	Elcarea int    `toml:"elcarea"`
	Elcbuis string `toml:"elcbuis"`
}

type oldArchiveMeta struct {
	StartTime   time.Time `toml:"start_time"`
	EndTime     time.Time `toml:"end_time"`
	ArchiveName string    `toml:"archive_name"`
	RecordsNum  int       `toml:"records_num"`
}

// Import 把旧数据导入空数据库. 只支持旧服务端使用的 CSV 与 TOML 布局.
func Import(ctx context.Context, db store.DB, opts Options) (Result, error) {
	var result Result
	if strings.TrimSpace(opts.DataDir) == "" || strings.TrimSpace(opts.ConfigDir) == "" {
		return result, errors.New("必须提供旧数据目录与旧配置目录")
	}
	if err := ensureEmpty(ctx, db); err != nil {
		return result, err
	}

	roomConfigPath := filepath.Join(opts.ConfigDir, "room.toml")
	data, err := os.ReadFile(roomConfigPath)
	if err != nil {
		return result, fmt.Errorf("读取旧房间配置失败: %w", err)
	}
	var oldRoom oldRoomConfig
	if err := toml.Unmarshal(data, &oldRoom); err != nil {
		return result, fmt.Errorf("解析旧房间配置失败: %w", err)
	}
	if strings.TrimSpace(oldRoom.RoomNo) == "" || strings.TrimSpace(oldRoom.Elcbuis) == "" || oldRoom.Elcarea < 0 {
		return result, errors.New("旧房间配置缺少有效的 roomNo, elcarea 或 elcbuis")
	}

	room, _, err := db.EnsureRoom(ctx, oldRoom.RoomNo, oldRoom.Elcarea, oldRoom.Elcbuis, time.Now())
	if err != nil {
		return result, fmt.Errorf("创建房间失败: %w", err)
	}
	if err := db.SetActiveRoom(ctx, room.ID); err != nil {
		return result, fmt.Errorf("设置活动房间失败: %w", err)
	}
	result.Rooms = 1

	roomDir := filepath.Join(opts.DataDir, "rooms", oldRoom.RoomNo)
	recordsPath := filepath.Join(roomDir, "records.csv")
	if _, statErr := os.Stat(recordsPath); errors.Is(statErr, os.ErrNotExist) {
		// 兼容更早的根目录 records.csv 布局.
		recordsPath = filepath.Join(opts.DataDir, "records.csv")
	}
	if _, statErr := os.Stat(recordsPath); statErr == nil {
		inserted, err := importSamples(ctx, db, room.ID, recordsPath)
		if err != nil {
			return result, err
		}
		result.Samples += inserted
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return result, statErr
	}

	archiveDir := filepath.Join(roomDir, "archives")
	entries, err := os.ReadDir(archiveDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("读取旧归档目录失败: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".toml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		metaPath := filepath.Join(archiveDir, name)
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			return result, fmt.Errorf("读取旧归档元数据 %s 失败: %w", name, err)
		}
		var meta oldArchiveMeta
		if err := toml.Unmarshal(raw, &meta); err != nil {
			return result, fmt.Errorf("解析旧归档元数据 %s 失败: %w", name, err)
		}
		archiveName := strings.TrimSpace(meta.ArchiveName)
		if archiveName == "" {
			archiveName = strings.TrimSuffix(name, filepath.Ext(name))
		}
		csvPath := filepath.Join(archiveDir, archiveName+".csv")
		if _, err := os.Stat(csvPath); err != nil {
			result.SkippedFiles++
			continue
		}
		if _, err := importSamples(ctx, db, room.ID, csvPath); err != nil {
			return result, err
		}
		created, err := db.CreateArchive(ctx, room.ID, archiveName, &meta.StartTime, &meta.EndTime, time.Now())
		if err != nil {
			if errors.Is(err, store.ErrArchiveEmpty) || errors.Is(err, store.ErrDuplicate) {
				result.SkippedFiles++
				continue
			}
			return result, fmt.Errorf("导入归档 %s 失败: %w", archiveName, err)
		}
		result.Archives++
		_ = created
	}

	// 旧 deleted 目录使用改名的 csv/toml, 无法可靠还原原始名称, 只统计跳过.
	if entries, err := os.ReadDir(filepath.Join(roomDir, "deleted")); err == nil {
		result.SkippedFiles += len(entries)
	}

	if entries, err := os.ReadDir(filepath.Join(opts.DataDir, "rooms")); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != oldRoom.RoomNo {
				result.SkippedRooms++
			}
		}
	}
	return result, nil
}

func ensureEmpty(ctx context.Context, db store.DB) error {
	stats, err := db.Stats(ctx)
	if err != nil {
		return err
	}
	rooms, err := db.ListRooms(ctx)
	if err != nil {
		return err
	}
	if stats.Samples > 0 || stats.Archives > 0 || len(rooms) > 0 {
		return errors.New("目标数据库不是空库, 为避免重复导入已中止")
	}
	return nil
}

func importSamples(ctx context.Context, db store.DB, roomID int64, path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("打开 %s 失败: %w", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = 2
	reader.TrimLeadingSpace = true
	inserted := 0
	line := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return inserted, fmt.Errorf("%s 第 %d 行解析失败: %w", path, line+1, err)
		}
		line++
		if len(record) == 0 || (len(record) == 1 && strings.TrimSpace(record[0]) == "") {
			continue
		}
		if len(record) != 2 {
			return inserted, fmt.Errorf("%s 第 %d 行应有两列", path, line)
		}
		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(record[0]))
		if err != nil {
			return inserted, fmt.Errorf("%s 第 %d 行时间格式错误: %w", path, line, err)
		}
		degree, err := strconv.ParseFloat(strings.TrimSpace(record[1]), 64)
		if err != nil {
			return inserted, fmt.Errorf("%s 第 %d 行电量格式错误: %w", path, line, err)
		}
		ok, err := db.InsertSample(ctx, roomID, ts, degree)
		if err != nil {
			return inserted, fmt.Errorf("写入 %s 第 %d 行失败: %w", path, line, err)
		}
		if ok {
			inserted++
		}
	}
	return inserted, nil
}
