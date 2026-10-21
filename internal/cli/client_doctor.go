package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/localapi"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func (a *App) newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "诊断配置, 网络与服务端状态",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadClientConfig(true)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			report := doctorReport{}
			add := func(name, status, detail string) {
				report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
			}

			// 1. 配置文件与权限.
			info, statErr := os.Stat(path)
			switch {
			case statErr != nil:
				add("config file", "fail", fmt.Sprintf("%s 不存在或不可读: %v", path, statErr))
			case info.Mode().Perm()&0o077 != 0:
				add("config file", "warn", fmt.Sprintf("%s (%04o, 权限过宽)", path, info.Mode().Perm()))
			default:
				add("config file", "ok", fmt.Sprintf("%s (%04o)", path, info.Mode().Perm()))
			}

			// 2. base url.
			u, urlErr := url.Parse(cfg.Server.BaseURL)
			if urlErr != nil || u.Host == "" {
				add("base url", "fail", "无法解析: "+cfg.Server.BaseURL)
			} else if u.Scheme != "http" && u.Scheme != "https" {
				add("base url", "fail", "必须使用 http 或 https")
			} else {
				detail := cfg.Server.BaseURL
				if strings.HasSuffix(cfg.Server.BaseURL, "/") {
					detail += " (尾部斜杠会被忽略)"
				}
				if u.Path != "" && u.Path != "/" {
					detail += " (带子路径)"
				}
				add("base url", "ok", detail)
			}

			// 3. token 格式.
			switch {
			case cfg.Server.Token == "":
				add("token", "warn", "未设置, 执行 dormwatt client config set-token")
			case !strings.HasPrefix(cfg.Server.Token, "dw_"):
				add("token", "fail", "token 前缀不是 dw_")
			default:
				add("token", "ok", tokenPrefixOf(cfg.Server.Token)+"...")
			}

			// 4. DNS 与 TCP.
			hostPort := hostPortOf(u)
			if urlErr == nil && u.Host != "" {
				if _, err := net.DefaultResolver.LookupHost(ctx, u.Hostname()); err != nil {
					add("dns", "fail", fmt.Sprintf("解析 %s 失败: %v", u.Hostname(), err))
				} else {
					add("dns", "ok", u.Hostname())
					conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", hostPort)
					if err != nil {
						add("tcp", "fail", fmt.Sprintf("连接 %s 失败: %v", hostPort, err))
					} else {
						conn.Close()
						add("tcp", "ok", hostPort)
					}
				}
			}

			// 5. TLS.
			if urlErr == nil && u.Scheme == "https" && u.Host != "" {
				expire, err := tlsExpiry(ctx, u)
				if err != nil {
					add("tls", "fail", err.Error())
				} else {
					add("tls", "ok", "证书有效至 "+expire.Format("2006-01-02"))
				}
			} else if urlErr == nil {
				add("tls", "warn", "使用明文 http")
			}

			// 6. health.
			client, clientErr := a.newUpstream(cfg)
			if clientErr != nil {
				add("health", "fail", clientErr.Error())
			} else if health, err := client.Health(ctx); err != nil {
				add("health", "fail", err.Error())
			} else {
				add("health", "ok", health.Version)
			}

			// 7. whoami 与 scope.
			var status *protocol.Status
			if client != nil {
				who, err := client.Whoami(ctx)
				if err != nil {
					add("whoami", "fail", err.Error())
				} else {
					scopes := strings.Join(who.Scopes, ",")
					if !hasScope(who.Scopes, "read") {
						add("whoami", "warn", fmt.Sprintf("%s (%s), 缺少 read scope", who.Name, scopes))
					} else {
						add("whoami", "ok", fmt.Sprintf("%s (%s)", who.Name, scopes))
					}
				}

				// 8. 服务端状态.
				st, err := client.Status(ctx)
				switch {
				case err != nil:
					add("status", "warn", err.Error())
				default:
					status = st
					switch {
					case st.ActiveRoom == nil:
						add("room", "warn", "未配置, 执行 dormwatt client room select")
					case st.Session.State == "missing" || st.Session.State == "invalid":
						add("session", "warn", "会话缺失或失效, 执行 dormwatt client login")
					case st.Monitor.LastError != nil:
						add("status", "warn", "上游错误: "+st.Monitor.LastError.Code)
					default:
						add("status", "ok", fmt.Sprintf("房间 %s, 会话 %s", st.ActiveRoom.RoomNo, st.Session.State))
					}
				}
			}

			// 9. 本地守护进程, 仅提示.
			if rtPath, err := appdirs.RuntimePath(); err == nil {
				if rt, err := localapi.ReadRuntime(rtPath); err == nil && localapi.RuntimeRunning(rt) {
					add("local daemon", "ok", fmt.Sprintf("运行中, 端口 %d", rt.Port))
				} else {
					add("local daemon", "warn", "未运行, 可选执行 dormwatt client serve")
				}
			}

			// 10. 系统通知.
			notifyStatus, notifyDetail := notificationCheck(cfg.Local.Notify)
			add("notifications", notifyStatus, notifyDetail)

			// 11. 存储统计.
			if status != nil {
				add("storage", "ok", fmt.Sprintf("samples=%d archives=%d", status.Storage.Samples, status.Storage.Archives))
			} else {
				add("storage", "warn", "无法读取服务端存储统计")
			}

			report.OK = true
			for _, c := range report.Checks {
				if c.Status == "fail" {
					report.OK = false
				}
			}
			if a.opts.json {
				if err := a.printJSON(report); err != nil {
					return exitErr(ExitGeneral, err)
				}
			} else {
				a.printDoctor(report)
			}
			if !report.OK {
				return &silentExit{Code: ExitNetwork}
			}
			return nil
		},
	}
}

func (a *App) printDoctor(report doctorReport) {
	width := 0
	for _, c := range report.Checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range report.Checks {
		tag := "[ok]  "
		switch c.Status {
		case "warn":
			tag = a.color.warn("[warn]")
		case "fail":
			tag = a.color.fail("[fail]")
		default:
			tag = a.color.ok("[ok]  ")
		}
		fmt.Fprintf(a.stdout, "%s %-*s %s\n", tag, width+1, c.Name+":", c.Detail)
	}
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want || scope == "admin" {
			return true
		}
	}
	return false
}

func hostPortOf(u *url.URL) string {
	if u == nil || u.Host == "" {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}
	port := "80"
	if u.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func tlsExpiry(ctx context.Context, u *url.URL) (time.Time, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		Config:    &tls.Config{ServerName: u.Hostname()},
	}
	conn, err := dialer.DialContext(ctx, "tcp", hostPortOf(u))
	if err != nil {
		return time.Time{}, fmt.Errorf("TLS 握手失败: %w", err)
	}
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return time.Time{}, fmt.Errorf("连接不是 TLS 连接")
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return time.Time{}, fmt.Errorf("服务端未提供证书")
	}
	return state.PeerCertificates[0].NotAfter, nil
}

func notificationCheck(enabled bool) (string, string) {
	if !enabled {
		return "warn", "已关闭 (local.notify=false)"
	}
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("terminal-notifier"); err == nil {
			return "ok", "terminal-notifier 通知与点击动作可用"
		}
		if _, err := exec.LookPath("osascript"); err == nil {
			return "ok", "osascript JXA 通知与点击动作可用"
		}
		return "warn", "未找到 osascript, 仅能使用基础通知"
	case "windows":
		if _, err := exec.LookPath("powershell"); err == nil {
			return "ok", "Toast 通知与点击动作可用"
		}
		return "warn", "未找到 powershell, 仅能使用基础通知"
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			return "ok", "notify-send 与点击动作可用"
		}
		return "warn", "未找到 notify-send, 仅能使用基础通知"
	default:
		return "ok", "使用通用通知"
	}
}
