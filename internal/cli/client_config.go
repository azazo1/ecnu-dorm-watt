package cli

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func (a *App) newClientConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "查看或修改客户端配置",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		a.newConfigShowCommand(),
		a.newConfigPathCommand(),
		a.newConfigSetServerCommand(),
		a.newConfigSetTokenCommand(),
		a.newConfigSetTimezoneCommand(),
		a.newConfigSetThresholdCommand(),
	)
	return cmd
}

func (a *App) newConfigShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "显示配置摘要",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			tokenSet := cfg.Server.Token != ""
			if a.opts.json {
				return a.printJSON(map[string]any{
					"config":       path,
					"server":       cfg.Server.BaseURL,
					"token_set":    tokenSet,
					"token_prefix": tokenPrefixOf(cfg.Server.Token),
					"timezone":     cfg.UI.Timezone,
					"local_bind":   cfg.Local.Bind,
					"local_tray":   cfg.Local.Tray,
					"local_notify": cfg.Local.Notify,
					"threshold":    cfg.Monitor.DegreeThreshold,
				})
			}
			tokenText := a.color.muted("(not set)")
			if tokenSet {
				tokenText = tokenPrefixOf(cfg.Server.Token) + "... (set)"
			}
			return a.printKV(a.stdout, [][2]string{
				{"config", path},
				{"server", cfg.Server.BaseURL},
				{"token", tokenText},
				{"timezone", cfg.UI.Timezone},
				{"local bind", cfg.Local.Bind},
				{"local tray", cfg.Local.Tray},
				{"local notify", fmt.Sprintf("%t", cfg.Local.Notify)},
				{"threshold", fmt.Sprintf("%.2f", cfg.Monitor.DegreeThreshold)},
			})
		},
	}
}

func (a *App) newConfigPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "输出配置文件路径",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := a.configPath()
			if err != nil {
				return exitErr(ExitUsage, err)
			}
			if a.opts.json {
				return a.printJSON(map[string]string{"config": path})
			}
			fmt.Fprintln(a.stdout, path)
			return nil
		},
	}
}

func (a *App) newConfigSetServerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-server <url>",
		Short: "设置服务端基址",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			normalized, err := normalizeServerURL(args[0])
			if err != nil {
				return usageErrorf("%v", err)
			}
			if u, _ := url.Parse(normalized); u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
				fmt.Fprintln(a.stderr, a.color.warn("警告:")+" 使用非回环的 http 地址, token 会以明文传输")
			}
			cfg, path, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			cfg.Server.BaseURL = normalized
			if err := a.saveClientConfig(path, cfg); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "config": path, "server": normalized})
			}
			fmt.Fprintln(a.stdout, "服务端地址已保存:", normalized)
			return nil
		},
	}
}

func (a *App) newConfigSetTokenCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-token",
		Short: "从 stdin 读取并保存服务端 token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isTerminalReader(a.stdin) {
				fmt.Fprintln(a.stderr, "提示: 建议使用管道输入, 例如 echo \"$TOKEN\" | dormwatt client config set-token")
			}
			line, err := readLineFrom(a.stdin)
			if err != nil {
				return usageErrorf("读取 token 失败: %v", err)
			}
			token := strings.TrimSpace(line)
			if err := validateServerToken(token); err != nil {
				return usageErrorf("%v", err)
			}
			cfg, path, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			cfg.Server.Token = token
			if err := a.saveClientConfig(path, cfg); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "config": path, "token_set": true})
			}
			fmt.Fprintf(a.stdout, "token 已保存到 %s, 权限 0600\n", path)
			return nil
		},
	}
}

func (a *App) newConfigSetTimezoneCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-timezone <tz>",
		Short: "设置展示时区",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tz := strings.TrimSpace(args[0])
			if _, err := time.LoadLocation(tz); err != nil {
				return usageErrorf("时区 %q 不是合法的 IANA 名称", tz)
			}
			cfg, path, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			cfg.UI.Timezone = tz
			if err := a.saveClientConfig(path, cfg); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "timezone": tz})
			}
			fmt.Fprintln(a.stdout, "时区已保存:", tz)
			return nil
		},
	}
}

func (a *App) newConfigSetThresholdCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-threshold <degree>",
		Short: "设置低电量阈值",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := strconv.ParseFloat(strings.TrimSpace(args[0]), 64)
			if err != nil {
				return usageErrorf("阈值必须是数字")
			}
			if threshold < 0 {
				return usageErrorf("阈值必须大于等于 0")
			}
			cfg, path, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			cfg.Monitor.DegreeThreshold = threshold
			if err := a.saveClientConfig(path, cfg); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "threshold": threshold})
			}
			fmt.Fprintf(a.stdout, "低电量阈值已保存: %.2f\n", threshold)
			return nil
		},
	}
}

func tokenPrefixOf(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 7 {
		return token
	}
	return token[:7]
}

func validateServerToken(token string) error {
	if !strings.HasPrefix(token, "dw_") {
		return fmt.Errorf("token 必须以 dw_ 开头")
	}
	if len(token) < 16 {
		return fmt.Errorf("token 长度不足, 至少 16 个字符")
	}
	if len(token) > 512 {
		return fmt.Errorf("token 长度超过 512 个字符")
	}
	return nil
}

func normalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("服务端地址不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("服务端地址无法解析: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("服务端地址必须使用 http 或 https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("服务端地址缺少主机名")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("服务端地址不允许包含查询参数或片段")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
