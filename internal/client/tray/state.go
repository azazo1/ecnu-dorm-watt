package tray

import "fmt"

// 健康状态取值, 与 localapi 的 Snapshot.Health 对应.
const (
	HealthStarting    = "starting"
	HealthOK          = "ok"
	HealthNoRoom      = "no_room"
	HealthNotLoggedIn = "not_logged_in"
	HealthServerDown  = "server_down"
	HealthNoNet       = "no_net"
	HealthTLSError    = "tls_error"
	HealthUpstreamErr = "upstream_error"
	HealthUnknown     = "unknown"
)

// State 是托盘展示所需的最小状态.
type State struct {
	Health    string
	Detail    string
	Degree    *float64
	Threshold float64
}

// Actions 是托盘菜单回调.
type Actions struct {
	OpenWeb   func()
	Refresh   func()
	LoginHelp func()
	Quit      func()
}

// StatusText 返回状态短句.
func StatusText(health string) string {
	switch health {
	case HealthOK:
		return "正常"
	case HealthNoRoom:
		return "未选择房间"
	case HealthNotLoggedIn:
		return "ECNU 登录已失效"
	case HealthServerDown:
		return "服务端不可达"
	case HealthNoNet:
		return "网络已断开"
	case HealthTLSError:
		return "TLS 校验失败"
	case HealthUpstreamErr:
		return "上游查询失败"
	case HealthStarting:
		return "启动中"
	default:
		return "状态未知"
	}
}

// LowBattery 判断是否低于阈值.
func (s State) LowBattery() bool {
	if s.Degree == nil || s.Threshold <= 0 {
		return false
	}
	return *s.Degree < s.Threshold
}

// DegreeText 返回电量文本, 没有数据时给出占位.
func (s State) DegreeText() string {
	if s.Degree == nil {
		return "当前电量: --"
	}
	return fmt.Sprintf("当前电量: %.2f 度", *s.Degree)
}

// FirstItem 返回菜单首项文本: 正常时显示电量, 其余情况显示状态.
func (s State) FirstItem() string {
	if s.Health == HealthOK {
		if s.LowBattery() {
			return s.DegreeText() + " (低电量)"
		}
		return s.DegreeText()
	}
	return StatusText(s.Health)
}

// StatusItem 返回状态项文本.
func (s State) StatusItem() string {
	return "状态: " + StatusText(s.Health)
}

// Tooltip 返回托盘提示文本.
func (s State) Tooltip() string { return s.TooltipFor("DormWatt") }

// TooltipFor 返回带指定应用名的托盘提示文本.
func (s State) TooltipFor(app string) string {
	if app == "" {
		app = "DormWatt"
	}
	if s.Health == HealthOK && s.Degree != nil {
		return fmt.Sprintf("%s: %.2f 度", app, *s.Degree)
	}
	return app + ": " + StatusText(s.Health)
}

// IconKind 返回图标颜色类别.
func (s State) IconKind() string {
	switch {
	case s.Health == HealthOK && s.LowBattery():
		return "warning"
	case s.Health == HealthOK:
		return "ok"
	case s.Health == HealthUpstreamErr:
		return "warning"
	case s.Health == HealthServerDown || s.Health == HealthNoNet || s.Health == HealthTLSError:
		return "error"
	default:
		return "muted"
	}
}
