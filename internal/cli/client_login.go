package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/login"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
)

func (a *App) newLoginCommand() *cobra.Command {
	var (
		manual      bool
		fromFile    string
		timeout     int
		browserPath string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "上传 ECNU 会话凭据并验证",
		Long: "默认启动本机 Chrome 完成学校 SSO, 然后读取会话凭据并上传.\n" +
			"无图形环境或浏览器不可用时, 使用 --manual 从 stdin 读取三行凭据,\n" +
			"或用 --from-file 从文件读取三行: JSESSIONID, cookie, X-CSRF-TOKEN.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fromFile != "" {
				manual = true
			}
			var values []string
			if manual {
				if fromFile != "" {
					data, err := os.ReadFile(fromFile)
					if err != nil {
						return exitErr(ExitUsage, fmt.Errorf("读取凭据文件失败: %w", err))
					}
					values, err = readAllLines(strings.NewReader(string(data)), 3)
					if err != nil || len(values) < 3 {
						return usageErrorf("凭据文件必须包含三行非空内容: JSESSIONID, cookie, X-CSRF-TOKEN")
					}
				} else {
					labels := []string{"JSESSIONID", "cookie", "X-CSRF-TOKEN"}
					for _, label := range labels {
						value, err := a.readSecretLine(label)
						if err != nil {
							return exitErr(ExitInterrupted, err)
						}
						values = append(values, value)
					}
				}
			} else {
				if browserPath == "" {
					browserPath = strings.TrimSpace(os.Getenv("DORMWATT_BROWSER"))
				}
				fmt.Fprintln(a.stderr, "正在启动浏览器, 请在窗口中完成 ECNU 登录...")
				creds, err := login.Browser(cmd.Context(), login.Options{
					BrowserPath: browserPath,
					Timeout:     time.Duration(timeout) * time.Second,
					Logf: func(format string, args ...any) {
						fmt.Fprintf(a.stderr, format+"\n", args...)
					},
				})
				if err != nil {
					return exitErr(ExitGeneral, fmt.Errorf("浏览器登录失败: %w; 可改用 dormwatt client login --manual", err))
				}
				values = []string{creds.JSESSIONID, creds.Cookie, creds.CSRFToken}
			}
			for _, value := range values {
				if value == "" {
					return usageErrorf("JSESSIONID, cookie 与 X-CSRF-TOKEN 均不能为空")
				}
				if len(value) > 8192 {
					return usageErrorf("会话字段长度不能超过 8192 字节")
				}
			}
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if timeout > 0 {
				var cancel func()
				ctx, cancel = timeoutContext(ctx, timeout)
				defer cancel()
			}
			if err := c.PutSession(ctx, upstream.SessionRequest{
				JSESSIONID: values[0],
				Cookie:     values[1],
				CSRFToken:  values[2],
			}); err != nil {
				return err
			}
			degree, verifyErr := c.Degree(ctx, true)
			if verifyErr != nil {
				a.log.Warn("会话已保存, 但验证失败", "error", verifyErr)
				return exitErr(classifyError(verifyErr), fmt.Errorf("会话已保存, 但验证失败: %w", verifyErr))
			}
			if a.opts.json {
				return a.printJSON(map[string]any{
					"ok":       true,
					"verified": true,
					"degree":   degree,
				})
			}
			loc := location(cfg.UI.Timezone)
			fmt.Fprintf(a.stdout, "登录成功, 当前剩余 %.2f 度 (查询于 %s)\n", degree.Degree, formatTime(degree.QueriedAt, loc))
			return nil
		},
	}
	cmd.Flags().BoolVar(&manual, "manual", false, "手动输入会话凭据, 不启动浏览器")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "从文件读取三行凭据")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "浏览器等待登录的秒数, 0 表示 5 分钟")
	cmd.Flags().StringVar(&browserPath, "browser-path", "", "Chrome 或 Chromium 可执行文件路径")
	return cmd
}

func (a *App) newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "清除服务端保存的 ECNU 会话",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			if err := c.DeleteSession(cmd.Context()); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true})
			}
			fmt.Fprintln(a.stdout, "服务端会话已清除")
			return nil
		},
	}
}
