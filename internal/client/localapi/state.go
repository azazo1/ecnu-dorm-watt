package localapi

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// 健康状态取值, 与设计文档 06 第 11 节一致.
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

const (
	// pollTimeout 是单次轮询的总超时.
	pollTimeout = 12 * time.Second
	// debounceCount 是 server_down 与 no_net 的展示阈值.
	debounceCount = 3
)

// Snapshot 是守护进程状态快照, 供本地 API 与托盘读取.
type Snapshot struct {
	Health      string           `json:"health"`
	Detail      string           `json:"detail,omitempty"`
	Degree      *float64         `json:"degree,omitempty"`
	Threshold   float64          `json:"threshold"`
	LastCheckAt *time.Time       `json:"last_check_at,omitempty"`
	Failures    int              `json:"consecutive_failures"`
	Remote      *protocol.Status `json:"remote,omitempty"`
}

type tracker struct {
	srv       *Server
	updates   chan Snapshot
	refreshCh chan struct{}

	refreshMu sync.Mutex
	mu        sync.Mutex
	snap      Snapshot

	pendingHealth string
	pendingCount  int

	lastLowNotify    time.Time
	lastRaiseNotify  time.Time
	lastHealthNotify map[string]time.Time
}

func newTracker(s *Server) *tracker {
	cfg := s.currentConfig()
	return &tracker{
		srv:              s,
		updates:          make(chan Snapshot, 1),
		refreshCh:        make(chan struct{}, 1),
		lastHealthNotify: make(map[string]time.Time),
		snap: Snapshot{
			Health:    HealthStarting,
			Detail:    "守护进程正在启动",
			Threshold: cfg.Monitor.DegreeThreshold,
		},
	}
}

func (t *tracker) start(ctx context.Context) {
	go t.run(ctx)
}

func (t *tracker) snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snap
}

func (t *tracker) requestRefresh() {
	select {
	case t.refreshCh <- struct{}{}:
	default:
	}
}

func (t *tracker) run(ctx context.Context) {
	t.refresh(ctx)
	for {
		interval := t.srv.currentConfig().Monitor.PollInterval.Std()
		if interval < time.Second {
			interval = time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-t.refreshCh:
			timer.Stop()
		}
		t.refresh(ctx)
	}
}

// refresh 执行一次健康检查与状态查询, 不触发上游 ECNU 查询.
func (t *tracker) refresh(ctx context.Context) {
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()

	now := t.srv.now()
	cfg := t.srv.currentConfig()
	client := t.srv.currentClient()
	next := Snapshot{Threshold: cfg.Monitor.DegreeThreshold, LastCheckAt: &now}

	health, detail := check(ctx, cfg, client, &next)
	next.Health = health
	next.Detail = detail
	switch health {
	case HealthServerDown, HealthNoNet, HealthTLSError:
		next.Failures = t.snapshot().Failures + 1
	default:
		next.Failures = 0
	}
	if next.Remote != nil && next.Remote.Monitor.LastDegree != nil {
		next.Degree = next.Remote.Monitor.LastDegree
	}
	t.apply(next)
}

// check 返回健康状态与短句说明, 并在成功时填充 next.Remote.
func check(ctx context.Context, cfg config.ClientConfig, client *upstream.Client, next *Snapshot) (string, string) {
	if strings.TrimSpace(cfg.Server.BaseURL) == "" || cfg.Server.Token == "" {
		return HealthUnknown, "尚未配置服务端地址或 token"
	}
	if client == nil {
		return HealthUnknown, "上游客户端未初始化"
	}
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	health, err := client.Health(ctx)
	if err != nil {
		if ae, ok := upstream.IsAPIError(err); ok {
			if ae.Status == 503 {
				return HealthServerDown, "服务端正在启动或关闭 (HTTP 503)"
			}
			return HealthUnknown, fmt.Sprintf("服务端 /v1/health 返回 HTTP %d (%s), 请检查 server.base_url 子路径", ae.Status, ae.Code())
		}
		return classifyTransport(ctx, err)
	}
	if health.Status != "ok" {
		return HealthServerDown, fmt.Sprintf("服务端状态为 %s", health.Status)
	}
	status, err := client.Status(ctx)
	if err != nil {
		if ae, ok := upstream.IsAPIError(err); ok {
			switch ae.Status {
			case 401:
				return HealthNotLoggedIn, "服务端 token 无效, 请执行 dormwatt client config set-token"
			case 403:
				return HealthNotLoggedIn, "服务端 token 权限不足"
			}
			return HealthUnknown, fmt.Sprintf("服务端返回 HTTP %d (%s)", ae.Status, ae.Code())
		}
		return classifyTransport(ctx, err)
	}
	next.Remote = status
	if status.ActiveRoom == nil {
		return HealthNoRoom, "尚未选择房间"
	}
	switch status.Session.State {
	case "missing", "invalid":
		return HealthNotLoggedIn, "ECNU 会话已失效, 请执行 dormwatt client login"
	}
	if status.Monitor.LastError != nil {
		msg := status.Monitor.LastError.Message
		if msg == "" {
			msg = status.Monitor.LastError.Code
		}
		return HealthUpstreamErr, "上游查询失败: " + msg
	}
	return HealthOK, ""
}

// classifyTransport 区分 TLS, 断网与服务端不可达.
func classifyTransport(ctx context.Context, err error) (string, string) {
	if upstream.IsTLS(err) {
		return HealthTLSError, "TLS 校验失败: " + err.Error()
	}
	if !probeNetwork(ctx) {
		return HealthNoNet, "本机网络不可用"
	}
	return HealthServerDown, "服务端不可达: " + err.Error()
}

// probeNetwork 探测本机是否可联网. 只用于区分 no_net 与 server_down.
func probeNetwork(ctx context.Context) bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	for _, dns := range []string{"223.5.5.5:53", "119.29.29.29:53"} {
		conn, err := dialer.DialContext(ctx, "tcp", dns)
		if err == nil {
			conn.Close()
			return true
		}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(lookupCtx, "www.baidu.com"); err == nil {
		return true
	}
	return false
}

// apply 应用去抖规则并发布快照.
func (t *tracker) apply(next Snapshot) {
	t.mu.Lock()
	prev := t.snap
	health := next.Health
	detail := next.Detail
	if health == HealthServerDown || health == HealthNoNet {
		if t.pendingHealth == health {
			t.pendingCount++
		} else {
			t.pendingHealth = health
			t.pendingCount = 1
		}
		if t.pendingCount < debounceCount && prev.Health != health {
			// 未达到阈值, 保持上一个展示状态, 但 detail 反映最近一次失败原因.
			health = prev.Health
		}
	} else {
		t.pendingHealth = ""
		t.pendingCount = 0
	}
	next.Health = health
	next.Detail = detail
	changed := prev.Health != next.Health || prev.Detail != next.Detail ||
		!equalFloatPtr(prev.Degree, next.Degree) || prev.Failures != next.Failures
	t.snap = next
	t.mu.Unlock()

	if prev.Health != next.Health {
		t.srv.log.Info("健康状态变化", "from", prev.Health, "to", next.Health, "detail", next.Detail)
	}
	if !changed {
		return
	}
	t.notify(prev, next)
	select {
	case t.updates <- next:
	default:
		select {
		case <-t.updates:
		default:
		}
		select {
		case t.updates <- next:
		default:
		}
	}
}

func (t *tracker) notify(prev, next Snapshot) {
	if t.srv.notifier == nil || !t.srv.currentConfig().Local.Notify {
		return
	}
	now := t.srv.now()
	if prev.Health != HealthStarting && next.Health != prev.Health {
		title, body, key := healthNotification(next.Health, next.Detail)
		if title != "" && now.Sub(t.lastHealthNotify[key]) >= 5*time.Minute {
			t.send(title, body)
			t.lastHealthNotify[key] = now
		}
	}
	if next.Degree == nil || next.Threshold <= 0 {
		return
	}
	low := *next.Degree < next.Threshold
	prevLow := prev.Degree != nil && *prev.Degree < prev.Threshold
	if low {
		if !prevLow || now.Sub(t.lastLowNotify) >= 30*time.Minute {
			t.send("宿舍电量不足", fmt.Sprintf("当前剩余 %.2f 度, 低于阈值 %.2f 度", *next.Degree, next.Threshold))
			t.lastLowNotify = now
		}
	} else {
		t.lastLowNotify = time.Time{}
	}
	if prev.Degree != nil && *next.Degree > *prev.Degree && now.Sub(t.lastRaiseNotify) >= 10*time.Minute {
		t.send("宿舍电量增加", fmt.Sprintf("电量从 %.2f 度增加到 %.2f 度", *prev.Degree, *next.Degree))
		t.lastRaiseNotify = now
	}
}

func (t *tracker) send(title, body string) {
	if err := t.srv.notifier.Notify(title, body); err != nil {
		t.srv.log.Warn("发送系统通知失败", "title", title, "error", err)
		return
	}
	t.srv.log.Info("已发送系统通知", "title", title)
}

func healthNotification(health, detail string) (title, body, key string) {
	switch health {
	case HealthNoRoom:
		return "DormWatt 未选择房间", firstNonEmpty(detail, "请在 Web 设置或 CLI 中选择宿舍房间"), health
	case HealthNotLoggedIn:
		return "ECNU 登录已失效", firstNonEmpty(detail, "请执行 dormwatt client login 重新登录"), health
	case HealthServerDown:
		return "服务端连接失败", firstNonEmpty(detail, "请检查服务端地址与运行状态"), health
	case HealthNoNet:
		return "网络已断开", firstNonEmpty(detail, "请检查本机网络"), health
	case HealthTLSError:
		return "TLS 校验失败", firstNonEmpty(detail, "请检查反向代理证书与系统时间"), health
	case HealthUpstreamErr:
		return "上游查询失败", firstNonEmpty(detail, "学校接口暂时异常, 请稍后重试"), health
	case HealthOK:
		return "DormWatt 已恢复", "服务端与 ECNU 会话恢复正常", health
	default:
		return "", "", health
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func equalFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
