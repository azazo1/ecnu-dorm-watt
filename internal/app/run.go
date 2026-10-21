package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/logging"
	"github.com/azazo1/ecnu-dorm-watt/internal/monitor"
	"github.com/azazo1/ecnu-dorm-watt/internal/server"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
	"github.com/azazo1/ecnu-dorm-watt/internal/version"
)

// ServerOptions 是启动服务端的参数.
type ServerOptions struct {
	ConfigPath string
}

// RunServer 启动服务端, 阻塞直到收到中断信号或发生致命错误.
func RunServer(opts ServerOptions) error {
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		p, err := appdirs.ServerConfigPath()
		if err != nil {
			return err
		}
		cfgPath = p
	}
	cfg, _, err := config.LoadServer(cfgPath, true)
	if err != nil {
		return fmt.Errorf("加载服务端配置: %w", err)
	}
	logDir, err := appdirs.LogDir()
	if err != nil {
		return err
	}
	logger, closeLog, err := logging.Setup(logging.Config{
		Level:      cfg.Log.Level,
		File:       cfg.Log.File,
		Dir:        logDir,
		Component:  "server",
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
	})
	if err != nil {
		return fmt.Errorf("初始化日志: %w", err)
	}
	defer closeLog()

	dataDir, err := appdirs.DataDir()
	if err != nil {
		return err
	}
	lock := flock.New(filepath.Join(dataDir, "server.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("获取单实例锁: %w", err)
	}
	if !locked {
		return fmt.Errorf("已有服务端实例在运行, 锁文件: %s", lock.Path())
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			logger.Warn("释放单实例锁失败", "err", err)
		}
	}()

	dbPath := cfg.Storage.Path
	if dbPath == "" {
		dbPath, err = appdirs.DatabasePath()
		if err != nil {
			return err
		}
	}
	db, err := store.Open(context.Background(), store.Options{Path: dbPath, BusyTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Warn("关闭数据库失败", "err", err)
		}
	}()

	ecnuClient, err := ecnu.New(ecnu.Options{
		Timeout:   cfg.ECNU.RequestTimeout.Std(),
		UserAgent: cfg.ECNU.UserAgent,
	})
	if err != nil {
		return fmt.Errorf("初始化 ECNU 客户端: %w", err)
	}
	mon, err := monitor.New(monitor.Options{
		DB:     db,
		Client: ecnuClient,
		Config: cfg.ECNU,
		Logger: logger.With("component", "monitor"),
	})
	if err != nil {
		return fmt.Errorf("初始化采集循环: %w", err)
	}
	catalog := NewCatalogSource(db, ecnuClient, cfg.ECNU.CatalogCache.Std())

	ver, commit, _ := version.Info()
	api := server.New(server.Deps{
		DB:           db,
		Config:       *cfg,
		Logger:       logger,
		Version:      ver,
		StartedAt:    time.Now(),
		StaticTokens: cfg.Auth.StaticTokens,
		Degree:       DegreeSource{Client: ecnuClient},
		Catalog:      catalog,
		Monitor:      mon,
	})

	ctx, stop := signalContext()
	defer stop()
	monDone := make(chan struct{})
	go func() {
		defer close(monDone)
		mon.Start(ctx)
	}()

	httpServer := &http.Server{
		Addr:              cfg.Server.Bind,
		Handler:           api.Handler(),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Std(),
		ReadTimeout:       cfg.Server.ReadTimeout.Std(),
		WriteTimeout:      cfg.Server.WriteTimeout.Std(),
		IdleTimeout:       cfg.Server.IdleTimeout.Std(),
		MaxHeaderBytes:    64 * 1024,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("服务端启动",
			"bind", cfg.Server.Bind,
			"base_path", api.BasePath(),
			"version", ver,
			"commit", commit,
			"database", dbPath,
		)
		var listenErr error
		if cfg.Server.TLS != nil && cfg.Server.TLS.Cert != "" {
			listenErr = httpServer.ListenAndServeTLS(cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
		} else {
			listenErr = httpServer.ListenAndServe()
		}
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("收到关闭信号")
	case err := <-errCh:
		logger.Error("HTTP 服务异常退出", "err", err)
		api.MarkShuttingDown()
		stop()
		<-monDone
		_ = httpServer.Close()
		return err
	}

	api.MarkShuttingDown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("优雅关闭超时, 强制关闭", "err", err)
		_ = httpServer.Close()
	}
	<-monDone
	logger.Info("服务端已关闭")
	return nil
}

// signalContext 返回可被 SIGINT/SIGTERM 取消的 context.
func signalContext() (context.Context, context.CancelFunc) {
	return notifyContext(context.Background())
}
