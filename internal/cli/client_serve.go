package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/localapi"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/notify"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/tray"
)

func (a *App) newServeCommand() *cobra.Command {
	var (
		port    int
		open    bool
		trayOpt string
		noTray  bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动本地守护进程, Web 宿主与系统托盘",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port != 0 && (port < 1 || port > 65535) {
				return usageErrorf("--port 必须在 1 到 65535 之间")
			}
			cfg, configPath, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			mode := trayOpt
			if noTray {
				mode = "off"
			}
			if mode == "" {
				mode = cfg.Local.Tray
			}
			switch mode {
			case "auto", "on", "off":
			default:
				return usageErrorf("--tray 必须是 auto, on 或 off")
			}

			bind := cfg.Local.Bind
			if port != 0 {
				host, _, err := net.SplitHostPort(bind)
				if err != nil {
					return usageErrorf("配置中的 local.bind 无效: %v", err)
				}
				bind = net.JoinHostPort(host, strconv.Itoa(port))
			}

			runtimePath, err := appdirs.RuntimePath()
			if err != nil {
				return exitErr(ExitGeneral, err)
			}
			if rt, err := localapi.ReadRuntime(runtimePath); err == nil {
				if localapi.RuntimeRunning(rt) {
					return exitErr(ExitDaemon, fmt.Errorf("本地守护进程已在运行 (PID %d, 端口 %d), 如需重启请先执行 dormwatt client quit", rt.PID, rt.Port))
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				a.log.Warn("运行文件无法解析, 将覆盖", "path", runtimePath, "error", err)
			}

			v, _, _ := versionInfo()
			var notifier localapi.Notifier
			if desktopSession() {
				if n, err := notify.New(localURL(bind)); err == nil {
					notifier = n
				} else {
					a.log.Warn("初始化系统通知失败", "error", err)
				}
			}
			srv, err := localapi.New(localapi.Options{
				Version:       v,
				ConfigPath:    configPath,
				Config:        cfg,
				Logger:        a.log,
				RuntimePath:   runtimePath,
				ListenAddress: bind,
				Notifier:      notifier,
			})
			if err != nil {
				return exitErr(ExitUsage, err)
			}
			if err := srv.Start(); err != nil {
				return exitErr(ExitDaemon, fmt.Errorf("%v\n请检查端口是否被占用, 或使用 --port 指定其他端口", err))
			}
			defer a.log.Info("本地服务已关闭")
			defer srv.Stop()

			ctx, stopSignals := signalContext()
			defer stopSignals()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			go func() {
				select {
				case <-srv.Done():
					cancel()
				case <-ctx.Done():
				}
			}()

			fmt.Fprintf(a.stdout, "本地界面: %s\n", srv.URL())
			a.log.Info("本地服务已就绪", "url", srv.URL(), "pid", os.Getpid())

			if open || cfg.Local.OpenBrowser {
				go func() {
					time.Sleep(150 * time.Millisecond)
					if err := openURL(srv.URL()); err != nil {
						a.log.Warn("打开浏览器失败", "error", err)
					}
				}()
			}

			hasDesktop := desktopSession()
			useTray := tray.Available() && (mode == "on" || (mode == "auto" && hasDesktop))
			if !tray.Available() && mode != "off" {
				a.log.Info("当前构建不含托盘支持, 以无托盘模式运行")
			} else if mode == "auto" && !hasDesktop {
				a.log.Info("未检测到桌面会话, 以无托盘模式运行")
			}
			if !useTray {
				select {
				case <-ctx.Done():
					a.log.Info("收到关闭信号")
				case <-srv.Done():
				}
			} else {
				if err := a.runTray(ctx, cancel, srv); err != nil {
					return err
				}
			}
			a.log.Info("正在关闭本地服务")
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "覆盖监听端口, 只改本次运行")
	cmd.Flags().BoolVar(&open, "open", false, "启动后打开默认浏览器")
	cmd.Flags().StringVar(&trayOpt, "tray", "", "托盘模式: auto, on, off, 默认取配置")
	cmd.Flags().BoolVar(&noTray, "no-tray", false, "等价于 --tray=off")
	return cmd
}

func (a *App) runTray(ctx context.Context, cancel context.CancelFunc, srv *localapi.Server) error {
	t := tray.New("DormWatt", tray.Actions{
		OpenWeb: func() {
			if err := openURL(srv.URL()); err != nil {
				a.log.Warn("打开浏览器失败", "error", err)
			}
		},
		Refresh: func() { srv.Refresh() },
		LoginHelp: func() {
			if err := openURL(srv.URL() + "settings#login"); err != nil {
				a.log.Warn("打开登录帮助失败", "error", err)
			}
		},
		Quit: cancel,
	})
	t.Update(toTrayState(srv.Snapshot()))
	updatesDone := make(chan struct{})
	go func() {
		defer close(updatesDone)
		for {
			select {
			case <-ctx.Done():
				return
			case snap, ok := <-srv.Subscribe():
				if !ok {
					return
				}
				t.Update(toTrayState(snap))
			}
		}
	}()
	err := t.Run(ctx)
	cancel()
	<-updatesDone
	return err
}

// desktopSession 粗略判断是否存在桌面会话.
func desktopSession() bool {
	switch runtime.GOOS {
	case "linux":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	case "darwin", "windows":
		return true
	default:
		return false
	}
}

// localURL 根据监听地址构造本地 Web 地址.
func localURL(bind string) string {
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "http://127.0.0.1:20007/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

func toTrayState(snap localapi.Snapshot) tray.State {
	state := tray.State{
		Health:    snap.Health,
		Detail:    snap.Detail,
		Degree:    snap.Degree,
		Threshold: snap.Threshold,
	}
	return state
}

func (a *App) newOpenCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "在默认浏览器中打开本地界面",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtimeInfo()
			if err != nil {
				return err
			}
			url := fmt.Sprintf("http://127.0.0.1:%d/", rt.Port)
			if err := openURL(url); err != nil {
				return exitErr(ExitGeneral, err)
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "url": url})
			}
			fmt.Fprintln(a.stdout, "已打开", url)
			return nil
		},
	}
}

func (a *App) newQuitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "quit",
		Short: "请求本地守护进程退出",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtimeInfo()
			if err != nil {
				return err
			}
			url := fmt.Sprintf("http://127.0.0.1:%d/api/shutdown", rt.Port)
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, url, strings.NewReader("{}"))
			if err != nil {
				return exitErr(ExitGeneral, err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Dormwatt-Client", "1")
			req.Header.Set("Authorization", "Bearer "+rt.Token)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return exitErr(ExitDaemon, fmt.Errorf("请求本地守护进程失败: %w", err))
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				return exitErr(ExitDaemon, fmt.Errorf("本地守护进程拒绝退出请求: HTTP %d", resp.StatusCode))
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "pid": rt.PID})
			}
			fmt.Fprintf(a.stdout, "已请求本地守护进程退出 (PID %d)\n", rt.PID)
			return nil
		},
	}
}

func (a *App) runtimeInfo() (*localapi.Runtime, error) {
	path, err := appdirs.RuntimePath()
	if err != nil {
		return nil, exitErr(ExitGeneral, err)
	}
	rt, err := localapi.ReadRuntime(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, exitErr(ExitDaemon, fmt.Errorf("本地守护进程未运行, 请先执行 dormwatt client serve"))
		}
		return nil, exitErr(ExitDaemon, fmt.Errorf("读取运行文件失败: %w", err))
	}
	if !localapi.RuntimeRunning(rt) {
		return nil, exitErr(ExitDaemon, fmt.Errorf("本地守护进程未运行 (PID %d 已退出), 请先执行 dormwatt client serve", rt.PID))
	}
	return rt, nil
}
