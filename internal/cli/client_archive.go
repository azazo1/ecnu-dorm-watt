package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

func (a *App) newArchiveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "管理电量归档",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		a.newArchiveListCommand(),
		a.newArchiveCreateCommand(),
		a.newArchiveDownloadCommand(),
		a.newArchiveDeleteCommand(),
	)
	return cmd
}

func roomQuery(all bool, roomID int64) string {
	switch {
	case all:
		return "all"
	case roomID > 0:
		return strconv.FormatInt(roomID, 10)
	default:
		return ""
	}
}

func (a *App) newArchiveListCommand() *cobra.Command {
	var (
		all            bool
		roomID         int64
		includeDeleted bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出归档",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			archives, err := c.Archives(cmd.Context(), roomQuery(all, roomID), includeDeleted)
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"archives": archives})
			}
			loc := location(cfg.UI.Timezone)
			rows := make([][]string, 0, len(archives))
			for _, meta := range archives {
				rows = append(rows, []string{
					meta.Name,
					formatTime(meta.StartTime, loc),
					formatTime(meta.EndTime, loc),
					fmt.Sprintf("%d", meta.RecordsNum),
					fmt.Sprintf("%d", meta.RoomID),
					formatTime(meta.CreatedAt, loc),
				})
			}
			return a.printTable(a.stdout, []string{"NAME", "START", "END", "RECORDS", "ROOM", "CREATED"}, rows)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "列出所有房间的归档")
	cmd.Flags().Int64Var(&roomID, "room", 0, "房间 id, 默认活动房间")
	cmd.Flags().BoolVar(&includeDeleted, "include-deleted", false, "包含软删除的归档")
	return cmd
}

func (a *App) newArchiveCreateCommand() *cobra.Command {
	var (
		from, to, name string
		roomID         int64
		yes            bool
		dryRun         bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "创建归档",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			req := upstream.ArchiveRequest{RoomID: roomID, Name: name}
			opts := upstream.RecordsOptions{RoomID: roomID, Limit: 200000}
			if from != "" {
				t, err := protocol.ParseTime(from)
				if err != nil {
					return usageErrorf("--from: %v", err)
				}
				req.From = protocol.FormatTime(t, t.Location())
				opts.From = &t
			}
			if to != "" {
				t, err := protocol.ParseTime(to)
				if err != nil {
					return usageErrorf("--to: %v", err)
				}
				req.To = protocol.FormatTime(t, t.Location())
				opts.To = &t
			}
			if opts.From != nil && opts.To != nil && opts.From.After(*opts.To) {
				return usageErrorf("--from 不能晚于 --to")
			}

			if dryRun {
				records, err := c.Records(cmd.Context(), opts)
				if err != nil {
					return err
				}
				if a.opts.json {
					return a.printJSON(map[string]any{
						"dry_run": true,
						"count":   len(records.Records),
						"from":    req.From,
						"to":      req.To,
					})
				}
				fmt.Fprintf(a.stdout, "将归档 %d 条样本", len(records.Records))
				if req.From != "" || req.To != "" {
					fmt.Fprintf(a.stdout, " (%s ~ %s)", orDash(req.From), orDash(req.To))
				}
				fmt.Fprintln(a.stdout, ", 未执行实际归档")
				return nil
			}

			if !yes {
				ok, err := a.confirm("归档会把该范围的样本从活动样本中移出, 属于破坏性操作. 继续?")
				if err != nil {
					return exitErr(ExitInterrupted, err)
				}
				if !ok {
					fmt.Fprintln(a.stdout, "已取消")
					return nil
				}
			}
			meta, err := c.CreateArchive(cmd.Context(), req)
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "archive": meta})
			}
			loc := location(cfg.UI.Timezone)
			fmt.Fprintf(a.stdout, "归档已创建: %s (%d 条, %s ~ %s)\n",
				meta.Name, meta.RecordsNum, formatTime(meta.StartTime, loc), formatTime(meta.EndTime, loc))
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "起始时间, RFC3339 带时区偏移")
	cmd.Flags().StringVar(&to, "to", "", "结束时间, RFC3339 带时区偏移")
	cmd.Flags().StringVar(&name, "name", "", "归档名称, 省略时由服务端生成")
	cmd.Flags().Int64Var(&roomID, "room", 0, "房间 id, 默认活动房间")
	cmd.Flags().BoolVar(&yes, "yes", false, "跳过确认")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只统计范围, 不执行归档")
	return cmd
}

func (a *App) newArchiveDownloadCommand() *cobra.Command {
	var (
		roomID int64
		out    string
	)
	cmd := &cobra.Command{
		Use:   "download <name>",
		Short: "下载归档 CSV",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			name := args[0]
			target := out
			if target == "" {
				target = name + ".csv"
			}
			w := io.Writer(a.stdout)
			var closer io.Closer
			if target != "-" {
				f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
				if err != nil {
					return exitErr(ExitGeneral, fmt.Errorf("创建输出文件失败: %w", err))
				}
				w = f
				closer = f
			}
			if closer != nil {
				defer closer.Close()
			}
			if err := c.DownloadArchive(cmd.Context(), name, roomQuery(false, roomID), w); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "out": target})
			}
			if target != "-" {
				fmt.Fprintf(a.stderr, "已写入 %s\n", target)
			}
			return nil
		},
	}
	cmd.Flags().Int64Var(&roomID, "room", 0, "归档所属房间 id, 默认活动房间")
	cmd.Flags().StringVar(&out, "out", "", "输出文件, 默认 <name>.csv, 使用 - 表示 stdout")
	return cmd
}

func (a *App) newArchiveDeleteCommand() *cobra.Command {
	var (
		roomID int64
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "删除归档",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			if !yes {
				ok, err := a.confirm(fmt.Sprintf("将软删除归档 %q, 数据仍保留在数据库中. 继续?", args[0]))
				if err != nil {
					return exitErr(ExitInterrupted, err)
				}
				if !ok {
					fmt.Fprintln(a.stdout, "已取消")
					return nil
				}
			}
			if err := c.DeleteArchive(cmd.Context(), args[0], roomQuery(false, roomID)); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "name": args[0]})
			}
			fmt.Fprintf(a.stdout, "归档 %q 已删除\n", args[0])
			return nil
		},
	}
	cmd.Flags().Int64Var(&roomID, "room", 0, "归档所属房间 id, 默认活动房间")
	cmd.Flags().BoolVar(&yes, "yes", false, "跳过确认")
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
