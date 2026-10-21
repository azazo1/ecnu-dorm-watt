// Package localapi 实现 dormwatt client serve 的本地 HTTP 服务.
//
// 它托管 Web 静态资源, 代理远端 /v1 API, 并提供本地状态与设置接口.
// 监听地址默认是 127.0.0.1:20007.
package localapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/client/webui"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
)

const (
	// DefaultBind 是默认监听地址.
	DefaultBind = "127.0.0.1:20007"
	// shutdownDelay 让 shutdown 响应先完成写出.
	shutdownDelay = 100 * time.Millisecond
)

// Notifier 发送桌面系统通知.
type Notifier interface {
	Notify(title, body string) error
}

// Options 是本地服务启动参数.
type Options struct {
	// Version 是客户端版本, 用于 /api/health 与 /api/status.
	Version string
	// ConfigPath 是 client.toml 路径, PUT /api/settings 会写回该文件.
	ConfigPath string
	// Config 是启动时的客户端配置.
	Config *config.ClientConfig
	// Logger 是日志器, 为空时丢弃日志.
	Logger *slog.Logger
	// RuntimePath 是 runtime.json 路径, 为空时不写运行文件.
	RuntimePath string
	// WebFS 是静态资源根, 为空时使用内嵌占位页面.
	WebFS fs.FS
	// ListenAddress 覆盖配置中的 local.bind, 仅影响本次运行.
	ListenAddress string
	// Now 便于测试注入时间, 为空时使用 time.Now.
	Now func() time.Time
	// Notifier 为空时关闭系统通知.
	Notifier Notifier
}

// Server 是本地守护进程的 HTTP 服务.
type Server struct {
	opts     Options
	log      *slog.Logger
	webFS    fs.FS
	now      func() time.Time
	runPath  string
	notifier Notifier

	cfg    atomic.Pointer[config.ClientConfig]
	client atomic.Pointer[upstream.Client]

	listenAddr string
	bindHost   string
	ln         net.Listener
	httpSrv    *http.Server
	port       int
	token      string
	startedAt  time.Time
	pid        int

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	tracker *tracker
}

// New 创建本地服务, 不开始监听.
func New(opts Options) (*Server, error) {
	cfg := config.DefaultClient()
	if opts.Config != nil {
		cfg = *opts.Config
	}
	if err := config.ValidateClient(&cfg); err != nil {
		return nil, fmt.Errorf("客户端配置无效: %w", err)
	}
	bind := strings.TrimSpace(opts.ListenAddress)
	if bind == "" {
		bind = cfg.Local.Bind
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return nil, fmt.Errorf("本地监听地址无效: %w", err)
	}
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	webFS := opts.WebFS
	if webFS == nil {
		webFS = webui.FS
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	runPath := opts.RuntimePath
	if runPath == "" {
		if p, err := defaultRuntimePath(); err == nil {
			runPath = p
		}
	}

	s := &Server{
		opts:       opts,
		log:        logger.With("component", "localapi"),
		webFS:      webFS,
		now:        now,
		runPath:    runPath,
		listenAddr: bind,
		bindHost:   host,
		token:      token,
		pid:        os.Getpid(),
		notifier:   opts.Notifier,
		done:       make(chan struct{}),
	}
	cfgCopy := cfg
	s.cfg.Store(&cfgCopy)
	client, err := upstream.New(upstream.Config{
		BaseURL: cfg.Server.BaseURL,
		Token:   cfg.Server.Token,
		Timeout: cfg.Server.RequestTimeout.Std(),
	})
	if err != nil {
		return nil, err
	}
	s.client.Store(client)
	s.tracker = newTracker(s)
	return s, nil
}

// Start 开始监听并写入 runtime.json.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", s.listenAddr, err)
	}
	s.ln = ln
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return fmt.Errorf("无法获取监听端口")
	}
	s.port = addr.Port
	s.startedAt = s.now()

	if s.runPath != "" {
		rt := Runtime{
			PID:       s.pid,
			Port:      s.port,
			Token:     s.token,
			StartedAt: s.startedAt,
		}
		if err := WriteRuntime(s.runPath, rt); err != nil {
			ln.Close()
			return fmt.Errorf("写入运行文件失败: %w", err)
		}
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.httpSrv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		err := s.httpSrv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("本地 HTTP 服务退出", "error", err)
		}
	}()
	s.tracker.start(s.ctx)
	s.log.Info("本地服务已启动", "url", s.URL(), "pid", s.pid, "token_prefix", TokenPrefixOf(s.token))
	return nil
}

// Shutdown 关闭本地服务并删除 runtime.json.
func (s *Server) Shutdown(ctx context.Context) {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.httpSrv != nil {
			if err := s.httpSrv.Shutdown(ctx); err != nil {
				s.log.Warn("本地 HTTP 服务未能优雅关闭", "error", err)
				if cerr := s.httpSrv.Close(); cerr != nil {
					s.log.Warn("强制关闭本地 HTTP 服务失败", "error", cerr)
				}
			}
		}
		if s.runPath != "" {
			if err := RemoveRuntime(s.runPath); err != nil {
				s.log.Warn("删除运行文件失败", "error", err)
			}
		}
		close(s.done)
	})
}

// Stop 是 Shutdown 的便捷封装, 最多等待 5 秒.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Shutdown(ctx)
}

// Done 在服务关闭后关闭.
func (s *Server) Done() <-chan struct{} { return s.done }

// URL 返回本地 Web 地址.
func (s *Server) URL() string {
	host := s.bindHost
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.port)) + "/"
}

// Port 返回实际监听端口.
func (s *Server) Port() int { return s.port }

// Token 返回本地 token, 仅用于 CLI 与托盘, 不要写入日志.
func (s *Server) Token() string { return s.token }

// RuntimePath 返回使用的 runtime.json 路径.
func (s *Server) RuntimePath() string { return s.runPath }

// Subscribe 返回状态更新通道, 通道中只保留最新状态.
func (s *Server) Subscribe() <-chan Snapshot { return s.tracker.updates }

// Snapshot 返回最近一次状态快照.
func (s *Server) Snapshot() Snapshot { return s.tracker.snapshot() }

// Refresh 触发一次后台状态轮询.
func (s *Server) Refresh() { s.tracker.requestRefresh() }

// currentConfig 返回当前配置副本.
func (s *Server) currentConfig() config.ClientConfig {
	if p := s.cfg.Load(); p != nil {
		return *p
	}
	return config.DefaultClient()
}

// currentClient 返回当前上游客户端.
func (s *Server) currentClient() *upstream.Client { return s.client.Load() }

// setConfig 替换配置并重建上游客户端.
func (s *Server) setConfig(cfg config.ClientConfig) error {
	client, err := upstream.New(upstream.Config{
		BaseURL: cfg.Server.BaseURL,
		Token:   cfg.Server.Token,
		Timeout: cfg.Server.RequestTimeout.Std(),
	})
	if err != nil {
		return err
	}
	s.cfg.Store(&cfg)
	s.client.Store(client)
	return nil
}
