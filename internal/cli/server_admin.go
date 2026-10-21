package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
	"github.com/azazo1/ecnu-dorm-watt/internal/legacy"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

func (a *App) newServerBackupCommand() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "备份数据库",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runServerBackup(out)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "备份输出路径")
	return cmd
}

func (a *App) runServerBackup(out string) error {
	if strings.TrimSpace(out) == "" {
		data, err := appdirs.DataDir()
		if err != nil {
			return exitErr(ExitGeneral, err)
		}
		out = filepath.Join(data, "backups", "dormwatt-"+time.Now().Format("20060102-150405")+".db")
	}
	_, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Backup(context.Background(), out); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return exitErr(ExitGeneral, err)
		}
		return exitErr(ExitGeneral, err)
	}
	if a.opts.json {
		return a.printJSON(map[string]any{"ok": true, "path": out})
	}
	fmt.Fprintf(a.stdout, "数据库已备份: %s\n", out)
	return nil
}

func (a *App) newServerCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "检查服务端配置与数据库",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runServerCheck()
		},
	}
}

func (a *App) runServerCheck() error {
	_, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	stats, err := db.Stats(ctx)
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	rooms, err := db.ListRooms(ctx)
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	tokens, err := db.ListTokens(ctx)
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	active, err := db.ActiveRoom(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return exitErr(ExitGeneral, err)
	}
	activeText := "-"
	if active != nil {
		activeText = fmt.Sprintf("%s (id=%d)", active.RoomNo, active.ID)
	}
	if a.opts.json {
		return a.printJSON(map[string]any{
			"config":      a.opts.configPath,
			"rooms":       len(rooms),
			"tokens":      len(tokens),
			"samples":     stats.Samples,
			"archives":    stats.Archives,
			"active_room": activeText,
		})
	}
	pairs := [][2]string{
		{"配置", a.opts.configPath},
		{"房间数", fmt.Sprintf("%d", len(rooms))},
		{"活动房间", activeText},
		{"token 数", fmt.Sprintf("%d", len(tokens))},
		{"样本数", fmt.Sprintf("%d", stats.Samples)},
		{"归档数", fmt.Sprintf("%d", stats.Archives)},
	}
	return a.printKV(a.stdout, pairs)
}

func (a *App) newServerPurgeCommand() *cobra.Command {
	var before string
	var yes bool
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "物理清理已删除的归档",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runServerPurge(before, yes)
		},
	}
	cmd.Flags().StringVar(&before, "deleted-before", "", "清理该时间之前删除的归档, RFC3339")
	cmd.Flags().BoolVar(&yes, "yes", false, "跳过确认")
	return cmd
}

func (a *App) runServerPurge(raw string, yes bool) error {
	if strings.TrimSpace(raw) == "" {
		return usageErrorf("必须提供 --deleted-before")
	}
	before, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return usageErrorf("--deleted-before 必须是带偏移的 RFC3339 时间: %v", err)
	}
	if !yes {
		ok, err := a.confirm("将物理删除 " + before.Format(time.RFC3339) + " 之前软删除的归档, 继续?")
		if err != nil {
			return exitErr(ExitGeneral, err)
		}
		if !ok {
			return nil
		}
	}
	_, db, err := a.openServerDB(context.Background())
	if err != nil {
		return err
	}
	defer db.Close()
	count, err := db.PurgeDeleted(context.Background(), before)
	if err != nil {
		return exitErr(ExitGeneral, err)
	}
	if a.opts.json {
		return a.printJSON(map[string]any{"ok": true, "deleted": count})
	}
	fmt.Fprintf(a.stdout, "已清理 %d 个归档\n", count)
	return nil
}

func (a *App) newServerImportLegacyCommand() *cobra.Command {
	var dataDir, configDir string
	cmd := &cobra.Command{
		Use:   "import-legacy",
		Short: "导入旧版数据",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataDir == "" || configDir == "" {
				return usageErrorf("必须提供 --data-dir 与 --config-dir")
			}
			ctx := context.Background()
			_, db, err := a.openServerDB(ctx)
			if err != nil {
				return err
			}
			defer db.Close()
			result, err := legacy.Import(ctx, db, legacy.Options{DataDir: dataDir, ConfigDir: configDir})
			if err != nil {
				return exitErr(ExitGeneral, fmt.Errorf("导入旧数据失败: %w", err))
			}
			if a.opts.json {
				return a.printJSON(map[string]any{
					"ok":            true,
					"rooms":         result.Rooms,
					"samples":       result.Samples,
					"archives":      result.Archives,
					"skipped_rooms": result.SkippedRooms,
					"skipped_files": result.SkippedFiles,
				})
			}
			pairs := [][2]string{
				{"房间", fmt.Sprintf("%d", result.Rooms)},
				{"样本", fmt.Sprintf("%d", result.Samples)},
				{"归档", fmt.Sprintf("%d", result.Archives)},
				{"跳过的房间", fmt.Sprintf("%d", result.SkippedRooms)},
				{"跳过的文件", fmt.Sprintf("%d", result.SkippedFiles)},
			}
			return a.printKV(a.stdout, pairs)
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "旧版数据目录")
	cmd.Flags().StringVar(&configDir, "config-dir", "", "旧版配置目录")
	return cmd
}
