package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

func (a *App) newWhoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "显示当前 token 的名称与 scope",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			who, err := c.Whoami(cmd.Context())
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(who)
			}
			return a.printKV(a.stdout, [][2]string{
				{"name", who.Name},
				{"scopes", strings.Join(who.Scopes, ", ")},
			})
		},
	}
}

func (a *App) newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "查看服务端状态摘要",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			status, err := c.Status(cmd.Context())
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(status)
			}
			loc := location(cfg.UI.Timezone)
			health := "ok"
			if h, err := c.Health(cmd.Context()); err != nil {
				health = "查询失败: " + err.Error()
			} else {
				health = h.Status
			}
			room := "not configured"
			if status.ActiveRoom != nil {
				room = fmt.Sprintf("%s (%d, %s)", status.ActiveRoom.RoomNo, status.ActiveRoom.Elcarea, status.ActiveRoom.Elcbuis)
			}
			session := status.Session.State
			if status.Session.UpdatedAt != nil {
				session += ", updated " + formatTime(*status.Session.UpdatedAt, loc)
			}
			monitor := status.Monitor.State
			if status.Monitor.LastSuccessAt != nil {
				monitor += ", last sample " + formatTime(*status.Monitor.LastSuccessAt, loc)
			}
			if status.Monitor.LastDegree != nil {
				monitor += fmt.Sprintf(", %.2f 度", *status.Monitor.LastDegree)
			}
			if status.Monitor.LastError != nil {
				monitor += ", 错误: " + status.Monitor.LastError.Code
				if status.Monitor.LastError.Message != "" {
					monitor += " " + status.Monitor.LastError.Message
				}
			}
			nextPoll := "-"
			if status.Monitor.NextPollAt != nil {
				nextPoll = formatTime(*status.Monitor.NextPollAt, loc)
			}
			pairs := [][2]string{
				{"server", fmt.Sprintf("%s (%s)", c.BaseURL(), status.Version)},
				{"health", health},
				{"room", room},
				{"session", session},
				{"monitor", monitor},
				{"storage", fmt.Sprintf("samples=%d archives=%d", status.Storage.Samples, status.Storage.Archives)},
				{"next poll", nextPoll},
			}
			return a.printKV(a.stdout, pairs)
		},
	}
}

func (a *App) newDegreeCommand() *cobra.Command {
	var fresh bool
	cmd := &cobra.Command{
		Use:   "degree",
		Short: "查询当前剩余电量",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			degree, err := c.Degree(cmd.Context(), fresh)
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(degree)
			}
			loc := location(cfg.UI.Timezone)
			source := degree.Source
			if degree.CachedUntil != nil {
				source += " (缓存到 " + formatTime(*degree.CachedUntil, loc) + ")"
			}
			return a.printKV(a.stdout, [][2]string{
				{"电量", fmt.Sprintf("%.2f 度", degree.Degree)},
				{"查询于", formatTime(degree.QueriedAt, loc)},
				{"来源", source},
			})
		},
	}
	cmd.Flags().BoolVar(&fresh, "fresh", false, "跳过服务端缓存, 直接查询上游")
	return cmd
}

func (a *App) newRecordsCommand() *cobra.Command {
	var (
		from   string
		to     string
		roomID int64
		format string
		out    string
		header bool
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "records",
		Short: "查询电量样本记录",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			opts := upstream.RecordsOptions{
				RoomID: roomID,
				Limit:  limit,
				Header: header,
			}
			if from != "" {
				t, err := protocol.ParseTime(from)
				if err != nil {
					return usageErrorf("--from: %v", err)
				}
				opts.From = &t
			}
			if to != "" {
				t, err := protocol.ParseTime(to)
				if err != nil {
					return usageErrorf("--to: %v", err)
				}
				opts.To = &t
			}
			if opts.From != nil && opts.To != nil && opts.From.After(*opts.To) {
				return usageErrorf("--from 不能晚于 --to")
			}
			format = strings.ToLower(format)
			if a.opts.json {
				format = "json"
			}
			switch format {
			case "table", "csv", "json":
			default:
				return usageErrorf("--format 必须是 table, csv 或 json")
			}

			w := io.Writer(a.stdout)
			var closer io.Closer
			if out != "" && out != "-" {
				f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
				if err != nil {
					return exitErr(ExitGeneral, fmt.Errorf("创建输出文件失败: %w", err))
				}
				w = f
				closer = f
			}
			if closer != nil {
				defer closer.Close()
			}

			loc := location(cfg.UI.Timezone)
			switch format {
			case "csv":
				if err := c.RecordsCSV(cmd.Context(), opts, w); err != nil {
					return err
				}
			case "json":
				records, err := c.Records(cmd.Context(), opts)
				if err != nil {
					return err
				}
				if err := writeJSONTo(w, records); err != nil {
					return exitErr(ExitGeneral, err)
				}
			case "table":
				records, err := c.Records(cmd.Context(), opts)
				if err != nil {
					return err
				}
				return a.printRecordsTable(w, records.Records, loc)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "起始时间, RFC3339 带时区偏移")
	cmd.Flags().StringVar(&to, "to", "", "结束时间, RFC3339 带时区偏移")
	cmd.Flags().Int64Var(&roomID, "room", 0, "房间 id, 默认活动房间")
	cmd.Flags().StringVar(&format, "format", "table", "输出格式: table, csv, json")
	cmd.Flags().StringVar(&out, "out", "", "写入文件而不是 stdout, 使用 - 表示 stdout")
	cmd.Flags().BoolVar(&header, "header", false, "CSV 输出表头")
	cmd.Flags().IntVar(&limit, "limit", 0, "最大记录数, 0 表示服务端默认值")
	return cmd
}

func (a *App) printRecordsTable(w io.Writer, records []protocol.Sample, loc *time.Location) error {
	daily := map[string]float64{}
	for i := 1; i < len(records); i++ {
		day := records[i].Time.In(loc).Format("2006-01-02")
		daily[day] += records[i-1].Degree - records[i].Degree
	}
	rows := make([][]string, 0, len(records))
	for i, s := range records {
		deltaText := "-"
		if i > 0 {
			deltaText = fmt.Sprintf("%.2f", records[i-1].Degree-s.Degree)
		}
		day := s.Time.In(loc).Format("2006-01-02")
		dailyText := "-"
		if v, ok := daily[day]; ok {
			dailyText = fmt.Sprintf("%.2f", v)
		}
		rows = append(rows, []string{
			formatTime(s.Time, loc),
			fmt.Sprintf("%.2f", s.Degree),
			deltaText,
			dailyText,
		})
	}
	return a.printTable(w, []string{"TIME", "DEGREE", "DELTA", "DAILY"}, rows)
}

func writeJSONTo(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
