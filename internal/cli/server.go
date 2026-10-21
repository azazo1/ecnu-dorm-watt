package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/app"
	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

func (a *App) newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "服务端管理命令",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		a.newServerRunCommand(),
		a.newServerTokenCommand(),
		a.newServerBackupCommand(),
		a.newServerCheckCommand(),
		a.newServerPurgeCommand(),
		a.newServerImportLegacyCommand(),
	)
	return cmd
}

func (a *App) newServerRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "启动服务端",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := a.serverConfigPath()
			if err != nil {
				return exitErr(ExitUsage, err)
			}
			if err := app.RunServer(app.ServerOptions{ConfigPath: path}); err != nil {
				return exitErr(ExitGeneral, err)
			}
			return nil
		},
	}
}

// serverConfigPath 返回服务端配置路径. 全局 --config 对 server 子命令同样生效.
func (a *App) serverConfigPath() (string, error) {
	if raw := strings.TrimSpace(a.opts.configPath); raw != "" {
		return raw, nil
	}
	return appdirs.ServerConfigPath()
}

// openServerDB 读取服务端配置并打开数据库.
func (a *App) openServerDB(ctx context.Context) (*config.ServerConfig, store.DB, error) {
	path, err := a.serverConfigPath()
	if err != nil {
		return nil, nil, exitErr(ExitUsage, err)
	}
	cfg, _, err := config.LoadServer(path, true)
	if err != nil {
		return nil, nil, exitErr(ExitUsage, fmt.Errorf("加载服务端配置: %w", err))
	}
	dbPath := strings.TrimSpace(cfg.Storage.Path)
	if dbPath == "" {
		dbPath, err = appdirs.DatabasePath()
		if err != nil {
			return nil, nil, exitErr(ExitGeneral, err)
		}
	}
	db, err := store.Open(ctx, store.Options{Path: dbPath, BusyTimeout: 5 * time.Second})
	if err != nil {
		return nil, nil, exitErr(ExitGeneral, fmt.Errorf("打开数据库: %w", err))
	}
	return cfg, db, nil
}
