package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/azazo1/ecnu-dorm-watt/internal/version"
)

// colorizer 控制 ANSI 颜色输出.
type colorizer struct {
	enabled bool
}

func (c colorizer) wrap(code, s string) string {
	if !c.enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (c colorizer) ok(s string) string    { return c.wrap("32", s) }
func (c colorizer) warn(s string) string  { return c.wrap("33", s) }
func (c colorizer) fail(s string) string  { return c.wrap("31", s) }
func (c colorizer) muted(s string) string { return c.wrap("90", s) }

// printJSON 输出单行 JSON.
func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// printError 输出错误. --json 时输出到 stdout, 否则输出到 stderr.
func (a *App) printError(err error) {
	if err == nil {
		return
	}
	var silent *silentExit
	if errors.As(err, &silent) {
		return
	}
	if a.opts.json {
		payload := map[string]any{
			"ok": false,
			"error": map[string]string{
				"code":    errorCode(err),
				"message": err.Error(),
			},
		}
		enc := json.NewEncoder(a.stdout)
		enc.SetEscapeHTML(false)
		if encErr := enc.Encode(payload); encErr != nil {
			fmt.Fprintln(a.stderr, err)
		}
		return
	}
	fmt.Fprintln(a.stderr, a.color.fail("错误:")+" "+err.Error())
}

// printTable 输出等宽表格.
func (a *App) printTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(headers) > 0 {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

// printKV 输出对齐的 key: value 行.
func (a *App) printKV(w io.Writer, pairs [][2]string) error {
	width := 0
	for _, p := range pairs {
		if n := utf8.RuneCountInString(p[0]); n > width {
			width = n
		}
	}
	for _, p := range pairs {
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width, p[0]+":", p[1]); err != nil {
			return err
		}
	}
	return nil
}

// formatTime 按给定时区格式化时间.
func formatTime(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}

// formatTimePtr 格式化可空时间, 空值返回占位符.
func formatTimePtr(t *time.Time, loc *time.Location) string {
	if t == nil {
		return "-"
	}
	return formatTime(*t, loc)
}

// location 读取配置时区, 失败时退回本地时区.
func location(cfgZone string) *time.Location {
	loc, err := time.LoadLocation(cfgZone)
	if err != nil {
		return time.Local
	}
	return loc
}

// versionInfo 返回版本信息.
func versionInfo() (string, string, string) {
	return version.Info()
}

// secondsDuration 把秒数转换为 time.Duration.
func secondsDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

// openBrowser 用系统默认浏览器打开地址.
func openBrowser(url string) error {
	return openURL(url)
}

// isTerminalWriter 判断输出是否连接到终端.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// isTerminalReader 判断输入是否来自终端.
func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// httpStatusError 把非 2xx 响应转换为错误.
func httpStatusError(resp *http.Response) error {
	return fmt.Errorf("本地请求返回 HTTP %d", resp.StatusCode)
}
