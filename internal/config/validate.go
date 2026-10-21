package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ValidateServer 校验服务端配置.
func ValidateServer(cfg *ServerConfig) error {
	if cfg.Version > CurrentVersion {
		return fmt.Errorf("config version %d is newer than supported %d", cfg.Version, CurrentVersion)
	}
	if _, _, err := net.SplitHostPort(cfg.Server.Bind); err != nil {
		return fmt.Errorf("server.bind: %w", err)
	}
	if cfg.Server.MaxBodyBytes <= 0 {
		return fmt.Errorf("server.max_body_bytes must be positive")
	}
	if cfg.Server.ReadHeaderTimeout.Std() <= 0 || cfg.Server.ReadTimeout.Std() <= 0 || cfg.Server.WriteTimeout.Std() <= 0 {
		return fmt.Errorf("server timeouts must be positive")
	}
	if cfg.ECNU.PollInterval.Std() < 10*time.Second || cfg.ECNU.PollInterval.Std() > time.Hour {
		return fmt.Errorf("ecnu.poll_interval must be between 10s and 1h")
	}
	if cfg.ECNU.RequestTimeout.Std() <= 0 {
		return fmt.Errorf("ecnu.request_timeout must be positive")
	}
	if cfg.ECNU.DegreeChangeThreshold < 0 {
		return fmt.Errorf("ecnu.degree_change_threshold must be >= 0")
	}
	if cfg.ECNU.UserAgent == "" {
		return fmt.Errorf("ecnu.user_agent is empty")
	}
	if cfg.UI.Timezone == "" {
		return fmt.Errorf("ui.timezone is empty")
	}
	if _, err := time.LoadLocation(cfg.UI.Timezone); err != nil {
		return fmt.Errorf("ui.timezone: %w", err)
	}
	if cfg.Server.TLS != nil {
		if cfg.Server.TLS.Cert == "" || cfg.Server.TLS.Key == "" {
			return fmt.Errorf("server.tls requires cert and key")
		}
	}
	return nil
}

// ValidateClient 校验客户端配置.
func ValidateClient(cfg *ClientConfig) error {
	if cfg.Version > CurrentVersion {
		return fmt.Errorf("config version %d is newer than supported %d", cfg.Version, CurrentVersion)
	}
	u, err := url.Parse(cfg.Server.BaseURL)
	if err != nil {
		return fmt.Errorf("server.base_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server.base_url must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("server.base_url has no host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("server.base_url must not contain query or fragment")
	}
	if strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
		cfg.Server.BaseURL = u.String()
	}
	if _, _, err := net.SplitHostPort(cfg.Local.Bind); err != nil {
		return fmt.Errorf("local.bind: %w", err)
	}
	if !cfg.Local.AllowRemote {
		host, _, _ := net.SplitHostPort(cfg.Local.Bind)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("local.bind must be loopback unless local.allow_remote is true")
		}
	}
	switch cfg.Local.Tray {
	case "auto", "on", "off":
	default:
		return fmt.Errorf("local.tray must be auto, on or off")
	}
	if cfg.Server.RequestTimeout.Std() <= 0 {
		return fmt.Errorf("server.request_timeout must be positive")
	}
	if cfg.Monitor.PollInterval.Std() < time.Second {
		return fmt.Errorf("monitor.poll_interval must be at least 1s")
	}
	if cfg.Monitor.DegreeThreshold < 0 {
		return fmt.Errorf("monitor.degree_threshold must be >= 0")
	}
	if cfg.UI.RefreshInterval.Std() < time.Second {
		return fmt.Errorf("ui.refresh_interval must be at least 1s")
	}
	if _, err := time.LoadLocation(cfg.UI.Timezone); err != nil {
		return fmt.Errorf("ui.timezone: %w", err)
	}
	return nil
}
