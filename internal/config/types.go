package config

import "time"

// Duration 是可写入 TOML 的时长类型.
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

type TLSConfig struct {
	Cert string `toml:"cert"`
	Key  string `toml:"key"`
}

type ServerSection struct {
	Bind              string     `toml:"bind"`
	AllowInsecureHTTP bool       `toml:"allow_insecure_http"`
	MaxBodyBytes      int64      `toml:"max_body_bytes"`
	ReadHeaderTimeout Duration   `toml:"read_header_timeout"`
	ReadTimeout       Duration   `toml:"read_timeout"`
	WriteTimeout      Duration   `toml:"write_timeout"`
	IdleTimeout       Duration   `toml:"idle_timeout"`
	TLS               *TLSConfig `toml:"tls"`
}

type StaticToken struct {
	Name   string   `toml:"name"`
	Scopes []string `toml:"scopes"`
	SHA256 string   `toml:"sha256"`
}

type AuthSection struct {
	TrustedProxies         []string      `toml:"trusted_proxies"`
	FailedAuthPerMinute    int           `toml:"failed_auth_per_minute"`
	RequestsPerMinute      int           `toml:"requests_per_minute"`
	UpstreamRequestsPerMin int           `toml:"upstream_requests_per_minute"`
	StaticTokens           []StaticToken `toml:"static_tokens"`
}

type ECNUSection struct {
	PollInterval          Duration `toml:"poll_interval"`
	RequestTimeout        Duration `toml:"request_timeout"`
	DegreeChangeThreshold float64  `toml:"degree_change_threshold"`
	LiveQueryCache        Duration `toml:"live_query_cache"`
	CatalogCache          Duration `toml:"catalog_cache"`
	PersistSession        bool     `toml:"persist_session"`
	UserAgent             string   `toml:"user_agent"`
}

type StorageSection struct {
	Path      string `toml:"path"`
	BackupDir string `toml:"backup_dir"`
}

type UISection struct {
	Timezone string `toml:"timezone"`
}

type LogSection struct {
	Level      string `toml:"level"`
	File       bool   `toml:"file"`
	MaxSizeMB  int    `toml:"max_size_mb"`
	MaxBackups int    `toml:"max_backups"`
}

type ServerConfig struct {
	Version int            `toml:"version"`
	Server  ServerSection  `toml:"server"`
	Auth    AuthSection    `toml:"auth"`
	ECNU    ECNUSection    `toml:"ecnu"`
	Storage StorageSection `toml:"storage"`
	UI      UISection      `toml:"ui"`
	Log     LogSection     `toml:"log"`
}

type ClientServerSection struct {
	BaseURL        string   `toml:"base_url"`
	Token          string   `toml:"token"`
	RequestTimeout Duration `toml:"request_timeout"`
}

type LocalSection struct {
	Bind        string `toml:"bind"`
	AllowRemote bool   `toml:"allow_remote"`
	OpenBrowser bool   `toml:"open_browser"`
	Tray        string `toml:"tray"`
	Notify      bool   `toml:"notify"`
}

type MonitorSection struct {
	PollInterval    Duration `toml:"poll_interval"`
	DegreeThreshold float64  `toml:"degree_threshold"`
}

type ClientUISection struct {
	Timezone        string   `toml:"timezone"`
	RefreshInterval Duration `toml:"refresh_interval"`
}

type ClientConfig struct {
	Version int                 `toml:"version"`
	Server  ClientServerSection `toml:"server"`
	Local   LocalSection        `toml:"local"`
	Monitor MonitorSection      `toml:"monitor"`
	UI      ClientUISection     `toml:"ui"`
	Log     LogSection          `toml:"log"`
}

// DefaultServer 返回服务端默认配置.
func DefaultServer() ServerConfig {
	return ServerConfig{
		Version: 1,
		Server: ServerSection{
			Bind:              "127.0.0.1:20006",
			AllowInsecureHTTP: false,
			MaxBodyBytes:      64 * 1024,
			ReadHeaderTimeout: Duration(10 * time.Second),
			ReadTimeout:       Duration(30 * time.Second),
			WriteTimeout:      Duration(60 * time.Second),
			IdleTimeout:       Duration(120 * time.Second),
		},
		Auth: AuthSection{
			TrustedProxies:         []string{"127.0.0.1/32", "::1/128"},
			FailedAuthPerMinute:    20,
			RequestsPerMinute:      300,
			UpstreamRequestsPerMin: 30,
		},
		ECNU: ECNUSection{
			PollInterval:          Duration(300 * time.Second),
			RequestTimeout:        Duration(15 * time.Second),
			DegreeChangeThreshold: 0.01,
			LiveQueryCache:        Duration(15 * time.Second),
			CatalogCache:          Duration(10 * time.Minute),
			PersistSession:        true,
			UserAgent:             "dormwatt/0.1",
		},
		UI:  UISection{Timezone: "Asia/Shanghai"},
		Log: LogSection{Level: "info", File: true, MaxSizeMB: 50, MaxBackups: 14},
	}
}

// DefaultClient 返回客户端默认配置.
func DefaultClient() ClientConfig {
	return ClientConfig{
		Version: 1,
		Server: ClientServerSection{
			BaseURL:        "http://127.0.0.1:20006",
			RequestTimeout: Duration(20 * time.Second),
		},
		Local: LocalSection{
			Bind:        "127.0.0.1:20007",
			OpenBrowser: true,
			Tray:        "auto",
			Notify:      true,
		},
		Monitor: MonitorSection{
			PollInterval:    Duration(10 * time.Second),
			DegreeThreshold: 10,
		},
		UI: ClientUISection{
			Timezone:        "Asia/Shanghai",
			RefreshInterval: Duration(5 * time.Second),
		},
		Log: LogSection{Level: "info", File: true, MaxSizeMB: 50, MaxBackups: 14},
	}
}
