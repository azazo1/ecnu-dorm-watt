package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
)

// App 保存命令执行所需的输入输出与全局选项.
type App struct {
	opts   globalOptions
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	log    *slog.Logger
	color  colorizer
}

type globalOptions struct {
	configPath string
	json       bool
	verbose    bool
	noColor    bool
}

// NewApp 创建使用标准输入输出的 App.
func NewApp() *App {
	a := &App{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
	a.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return a
}

// Execute 执行命令行并返回退出码.
func Execute(args []string) int {
	return NewApp().Execute(args)
}

// Execute 执行命令行并返回退出码.
func (a *App) Execute(args []string) int {
	root := a.rootCommand()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		a.printError(err)
		return classifyError(err)
	}
	return ExitOK
}

func (a *App) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "dormwatt",
		Short:         "华东师范大学宿舍电量工具",
		Long:          "dormwatt 提供宿舍电量查询, 记录归档与本地 Web 界面.\n客户端命令位于 dormwatt client 下.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			a.initLogging()
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetIn(a.stdin)

	version, _, _ := versionInfo()
	root.Version = version
	root.SetVersionTemplate("dormwatt {{.Version}}\n")

	pf := root.PersistentFlags()
	pf.StringVar(&a.opts.configPath, "config", os.Getenv("DORMWATT_CONFIG"), "配置文件路径, 默认按子命令使用 client.toml 或 server.toml")
	pf.BoolVar(&a.opts.json, "json", false, "输出机器可读的 JSON")
	pf.BoolVar(&a.opts.verbose, "verbose", false, "输出 debug 日志到 stderr")
	pf.BoolVar(&a.opts.noColor, "no-color", false, "关闭颜色输出")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageErrorf("%v", err)
	})

	root.AddCommand(
		a.newVersionCommand(),
		a.newClientCommand(),
		a.newServerCommand(),
	)
	return root
}

func (a *App) initLogging() {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DORMWATT_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	if a.opts.verbose {
		level = slog.LevelDebug
	}
	a.log = slog.New(slog.NewTextHandler(a.stderr, &slog.HandlerOptions{Level: level}))
	a.color = colorizer{
		enabled: !a.opts.noColor && os.Getenv("NO_COLOR") == "",
	}
	if a.color.enabled {
		if f, ok := a.stdout.(*os.File); ok {
			info, err := f.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				a.color.enabled = false
			}
		} else {
			a.color.enabled = false
		}
	}
}

func (a *App) newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "显示版本信息",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, commit, built := versionInfo()
			if a.opts.json {
				return a.printJSON(map[string]string{
					"version": v,
					"commit":  commit,
					"built":   built,
				})
			}
			fmt.Fprintf(a.stdout, "dormwatt %s (%s, %s)\n", v, commit, built)
			return nil
		},
	}
	return cmd
}

// configPath 返回本次运行使用的客户端配置路径.
func (a *App) configPath() (string, error) {
	raw := strings.TrimSpace(a.opts.configPath)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("DORMWATT_CONFIG"))
	}
	if raw == "" {
		p, err := appdirs.ClientConfigPath()
		if err != nil {
			return "", err
		}
		return p, nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("解析配置路径失败: %w", err)
	}
	return abs, nil
}

// loadClientConfig 读取客户端配置并应用环境变量覆盖.
func (a *App) loadClientConfig(create bool) (*config.ClientConfig, string, error) {
	path, err := a.configPath()
	if err != nil {
		return nil, "", exitErr(ExitUsage, err)
	}
	cfg, _, err := config.LoadClient(path, create)
	if err != nil {
		return nil, "", usageErrorf("读取配置 %s 失败: %v", path, err)
	}
	if v := strings.TrimSpace(os.Getenv("DORMWATT_SERVER")); v != "" {
		cfg.Server.BaseURL = strings.TrimSuffix(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("DORMWATT_TOKEN")); v != "" {
		cfg.Server.Token = v
	}
	a.log.Debug("已加载客户端配置", "path", path, "base_url", cfg.Server.BaseURL)
	return cfg, path, nil
}

// newUpstream 根据配置创建上游客户端.
func (a *App) newUpstream(cfg *config.ClientConfig) (*upstream.Client, error) {
	c, err := upstream.New(upstream.Config{
		BaseURL: cfg.Server.BaseURL,
		Token:   cfg.Server.Token,
		Timeout: cfg.Server.RequestTimeout.Std(),
	})
	if err != nil {
		return nil, usageErrorf("%v", err)
	}
	return c, nil
}

var (
	noArgs = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usageErrorf("命令 %s 不接受位置参数", cmd.CommandPath())
		}
		return nil
	}
	exactArgs = func(n int) cobra.PositionalArgs {
		return func(cmd *cobra.Command, args []string) error {
			if len(args) != n {
				return usageErrorf("命令 %s 需要 %d 个位置参数, 实际 %d 个", cmd.CommandPath(), n, len(args))
			}
			return nil
		}
	}
	rangeArgs = func(min, max int) cobra.PositionalArgs {
		return func(cmd *cobra.Command, args []string) error {
			if len(args) < min || len(args) > max {
				return usageErrorf("命令 %s 需要 %d 到 %d 个位置参数, 实际 %d 个", cmd.CommandPath(), min, max, len(args))
			}
			return nil
		}
	}
)

// timeoutContext 返回带超时的上下文, 便于 HTTP 命令统一控制.
func timeoutContext(ctx context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, secondsDuration(seconds))
}
