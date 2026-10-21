package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// ServerSettings 是 GET /api/settings 的服务端连接部分.
type ServerSettings struct {
	BaseURL        string `json:"base_url"`
	TokenSet       bool   `json:"token_set"`
	TokenPrefix    string `json:"token_prefix,omitempty"`
	RequestTimeout string `json:"request_timeout"`
}

// LocalSettings 是本地监听部分.
type LocalSettings struct {
	Bind        string `json:"bind"`
	AllowRemote bool   `json:"allow_remote"`
	OpenBrowser bool   `json:"open_browser"`
	Tray        string `json:"tray"`
	Notify      bool   `json:"notify"`
}

// MonitorSettings 是轮询与阈值部分.
type MonitorSettings struct {
	PollInterval    string  `json:"poll_interval"`
	DegreeThreshold float64 `json:"degree_threshold"`
}

// UISettings 是展示部分.
type UISettings struct {
	Timezone        string `json:"timezone"`
	RefreshInterval string `json:"refresh_interval"`
}

// SettingsView 是本地设置响应. token 永远不返回原值.
type SettingsView struct {
	Server          ServerSettings  `json:"server"`
	Local           LocalSettings   `json:"local"`
	Monitor         MonitorSettings `json:"monitor"`
	UI              UISettings      `json:"ui"`
	RestartRequired bool            `json:"restart_required,omitempty"`
}

func settingsFromConfig(cfg config.ClientConfig) SettingsView {
	return SettingsView{
		Server: ServerSettings{
			BaseURL:        cfg.Server.BaseURL,
			TokenSet:       cfg.Server.Token != "",
			TokenPrefix:    tokenPrefix(cfg.Server.Token),
			RequestTimeout: cfg.Server.RequestTimeout.Std().String(),
		},
		Local: LocalSettings{
			Bind:        cfg.Local.Bind,
			AllowRemote: cfg.Local.AllowRemote,
			OpenBrowser: cfg.Local.OpenBrowser,
			Tray:        cfg.Local.Tray,
			Notify:      cfg.Local.Notify,
		},
		Monitor: MonitorSettings{
			PollInterval:    cfg.Monitor.PollInterval.Std().String(),
			DegreeThreshold: cfg.Monitor.DegreeThreshold,
		},
		UI: UISettings{
			Timezone:        cfg.UI.Timezone,
			RefreshInterval: cfg.UI.RefreshInterval.Std().String(),
		},
	}
}

func tokenPrefix(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 7 {
		return token
	}
	return token[:7]
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request, rid string) {
	writeJSON(w, http.StatusOK, settingsFromConfig(s.currentConfig()))
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request, rid string) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var patch settingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, rid, http.StatusBadRequest, protocol.CodeInvalidRequest, "设置无法解析", err.Error())
		return
	}
	cfg := s.currentConfig()
	oldBind := cfg.Local.Bind
	if err := applySettingsPatch(&cfg, patch); err != nil {
		writeProblem(w, rid, http.StatusUnprocessableEntity, protocol.CodeValidationFailed, "设置校验失败", err.Error())
		return
	}
	if err := config.ValidateClient(&cfg); err != nil {
		writeProblem(w, rid, http.StatusUnprocessableEntity, protocol.CodeValidationFailed, "设置校验失败", err.Error())
		return
	}
	if s.opts.ConfigPath == "" {
		writeProblem(w, rid, http.StatusInternalServerError, "config_write_failed", "配置写入失败", "未指定配置文件路径")
		return
	}
	if err := config.WriteClient(s.opts.ConfigPath, &cfg); err != nil {
		writeProblem(w, rid, http.StatusInternalServerError, "config_write_failed", "配置写入失败", err.Error())
		return
	}
	if err := s.setConfig(cfg); err != nil {
		writeProblem(w, rid, http.StatusInternalServerError, protocol.CodeInternal, "上游客户端重建失败", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	s.tracker.refresh(ctx)

	view := settingsFromConfig(s.currentConfig())
	view.RestartRequired = cfg.Local.Bind != oldBind
	writeJSON(w, http.StatusOK, view)
}

type optionalString struct {
	Set   bool
	Null  bool
	Value string
}

func (o *optionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = v
	return nil
}

type settingsPatch struct {
	Server  *serverPatch  `json:"server"`
	Local   *localPatch   `json:"local"`
	Monitor *monitorPatch `json:"monitor"`
	UI      *uiPatch      `json:"ui"`
}

type serverPatch struct {
	BaseURL        *string        `json:"base_url"`
	Token          optionalString `json:"token"`
	RequestTimeout *string        `json:"request_timeout"`
}

type localPatch struct {
	Bind        *string `json:"bind"`
	AllowRemote *bool   `json:"allow_remote"`
	OpenBrowser *bool   `json:"open_browser"`
	Tray        *string `json:"tray"`
	Notify      *bool   `json:"notify"`
}

type monitorPatch struct {
	PollInterval    *string  `json:"poll_interval"`
	DegreeThreshold *float64 `json:"degree_threshold"`
}

type uiPatch struct {
	Timezone        *string `json:"timezone"`
	RefreshInterval *string `json:"refresh_interval"`
}

func applySettingsPatch(cfg *config.ClientConfig, p settingsPatch) error {
	if p.Server != nil {
		if p.Server.BaseURL != nil {
			normalized, err := normalizeBaseURL(*p.Server.BaseURL)
			if err != nil {
				return err
			}
			cfg.Server.BaseURL = normalized
		}
		if p.Server.Token.Set {
			switch {
			case p.Server.Token.Null:
				cfg.Server.Token = ""
			case strings.TrimSpace(p.Server.Token.Value) == "":
				// 空字符串表示不修改.
			default:
				token := strings.TrimSpace(p.Server.Token.Value)
				if !strings.HasPrefix(token, "dw_") {
					return fmt.Errorf("token 必须以 dw_ 开头")
				}
				if len(token) < 16 {
					return fmt.Errorf("token 长度不足")
				}
				cfg.Server.Token = token
			}
		}
		if p.Server.RequestTimeout != nil {
			d, err := time.ParseDuration(*p.Server.RequestTimeout)
			if err != nil || d <= 0 {
				return fmt.Errorf("server.request_timeout 必须为合法正时长")
			}
			cfg.Server.RequestTimeout = config.Duration(d)
		}
	}
	if p.Local != nil {
		if p.Local.Bind != nil {
			cfg.Local.Bind = strings.TrimSpace(*p.Local.Bind)
		}
		if p.Local.AllowRemote != nil {
			cfg.Local.AllowRemote = *p.Local.AllowRemote
		}
		if p.Local.OpenBrowser != nil {
			cfg.Local.OpenBrowser = *p.Local.OpenBrowser
		}
		if p.Local.Tray != nil {
			cfg.Local.Tray = *p.Local.Tray
		}
		if p.Local.Notify != nil {
			cfg.Local.Notify = *p.Local.Notify
		}
	}
	if p.Monitor != nil {
		if p.Monitor.PollInterval != nil {
			d, err := time.ParseDuration(*p.Monitor.PollInterval)
			if err != nil || d < time.Second {
				return fmt.Errorf("monitor.poll_interval 必须为至少 1s 的合法时长")
			}
			cfg.Monitor.PollInterval = config.Duration(d)
		}
		if p.Monitor.DegreeThreshold != nil {
			cfg.Monitor.DegreeThreshold = *p.Monitor.DegreeThreshold
		}
	}
	if p.UI != nil {
		if p.UI.Timezone != nil {
			cfg.UI.Timezone = strings.TrimSpace(*p.UI.Timezone)
		}
		if p.UI.RefreshInterval != nil {
			d, err := time.ParseDuration(*p.UI.RefreshInterval)
			if err != nil || d < time.Second {
				return fmt.Errorf("ui.refresh_interval 必须为至少 1s 的合法时长")
			}
			cfg.UI.RefreshInterval = config.Duration(d)
		}
	}
	return nil
}

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("server.base_url 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("server.base_url 无法解析: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("server.base_url 必须使用 http 或 https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("server.base_url 缺少主机名")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("server.base_url 不允许包含查询参数或片段")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}
