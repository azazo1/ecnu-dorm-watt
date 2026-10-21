package cli

import (
	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
)

// newClientCommand 创建 client 子命令树.
func (a *App) newClientCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "客户端命令",
		Long:  "客户端命令直接访问服务端 /v1 API, 不要求本地守护进程运行.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		a.newClientConfigCommand(),
		a.newDoctorCommand(),
		a.newWhoamiCommand(),
		a.newStatusCommand(),
		a.newDegreeCommand(),
		a.newRecordsCommand(),
		a.newRoomCommand(),
		a.newLoginCommand(),
		a.newLogoutCommand(),
		a.newArchiveCommand(),
		a.newServeCommand(),
		a.newOpenCommand(),
		a.newQuitCommand(),
	)
	return cmd
}

// clientFor 读取配置并创建上游客户端.
func (a *App) clientFor() (*upstream.Client, *config.ClientConfig, string, error) {
	cfg, path, err := a.loadClientConfig(true)
	if err != nil {
		return nil, nil, "", err
	}
	c, err := a.newUpstream(cfg)
	if err != nil {
		return nil, nil, "", err
	}
	return c, cfg, path, nil
}

// saveClientConfig 校验并写入客户端配置.
func (a *App) saveClientConfig(path string, cfg *config.ClientConfig) error {
	if err := config.ValidateClient(cfg); err != nil {
		return usageErrorf("配置校验失败: %v", err)
	}
	if err := config.WriteClient(path, cfg); err != nil {
		return exitErr(ExitGeneral, err)
	}
	return nil
}
