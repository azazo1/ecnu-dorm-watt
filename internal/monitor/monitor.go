// Package monitor 实现服务端采集循环与采集状态机.
//
// 循环负责读取活动房间与会话, 调用 ECNU 上游, 判断是否记录样本, 维护状态字段,
// 并按退避与抖动安排下一次查询. 所有状态只保存在内存.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// 采集状态机取值, 与 protocol.MonitorStatus.State 一致.
const (
	StateIdle      = "idle"
	StateNoSession = "no_session"
	StateActive    = "active"
	StateDegraded  = "degraded"
)

const (
	// DefaultPollInterval 是未配置时的采集周期.
	DefaultPollInterval = 300 * time.Second
	// MaxBackoff 是失败退避上限.
	MaxBackoff = 30 * time.Minute
	// SessionProbeInterval 是会话失效时的最小探测间隔.
	SessionProbeInterval = 15 * time.Minute
	// DefaultThreshold 是未配置时使用的度数变化阈值.
	DefaultThreshold = 0.01

	firstDelayMax   = 2 * time.Second
	maxMessageRunes = 200
)

// Options 是构造 Monitor 的配置.
type Options struct {
	DB     store.DB
	Client *ecnu.Client
	Config config.ECNUSection
	// Logger 为空时使用 slog.Default.
	Logger *slog.Logger
	// Now 可注入时间源, 为空时使用 time.Now.
	Now func() time.Time
	// StartedAt 可指定开始时间, 为零时在 Start 时取当前时间.
	StartedAt time.Time
}

type runtimeState struct {
	state       string
	startedAt   time.Time
	lastAttempt *time.Time
	lastSuccess *time.Time
	lastDegree  *float64
	lastError   *protocol.ErrorInfo
	nextPoll    *time.Time
	failures    int
}

// Monitor 是采集循环.
//
// Start 会阻塞直到 ctx 结束, 调用方应在单独的 goroutine 中运行.
// Monitor 可被并发调用 Status 与 Wake.
type Monitor struct {
	db     store.DB
	client *ecnu.Client
	cfg    config.ECNUSection
	log    *slog.Logger
	now    func() time.Time
	wake   chan struct{}

	mu sync.Mutex
	st runtimeState
}

// New 构造 Monitor, 不启动循环.
func New(opts Options) (*Monitor, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("monitor: 缺少数据库")
	}
	if opts.Client == nil {
		return nil, fmt.Errorf("monitor: 缺少 ECNU 客户端")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Monitor{
		db:     opts.DB,
		client: opts.Client,
		cfg:    opts.Config,
		log:    logger,
		now:    now,
		wake:   make(chan struct{}, 1),
		st: runtimeState{
			state:     StateIdle,
			startedAt: opts.StartedAt,
		},
	}, nil
}

// Start 启动采集循环, 直到 ctx 被取消.
func (m *Monitor) Start(ctx context.Context) {
	m.mu.Lock()
	if m.st.startedAt.IsZero() {
		m.st.startedAt = m.now()
	}
	startedAt := m.st.startedAt
	m.mu.Unlock()
	m.log.Info("采集循环启动", "started_at", startedAt, "poll_interval", m.pollInterval())

	delay := m.initialDelay()
	for {
		m.planNext(delay)
		if !m.wait(ctx, delay) {
			m.log.Info("采集循环停止")
			return
		}
		delay = m.poll(ctx)
		if ctx.Err() != nil {
			m.log.Info("采集循环停止")
			return
		}
	}
}

// Wake 唤醒循环立即执行一次查询, 供房间切换与会话上传使用.
// 多次调用会被合并, 不会阻塞.
func (m *Monitor) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Status 返回当前采集状态快照.
func (m *Monitor) Status() protocol.MonitorStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return protocol.MonitorStatus{
		PollIntervalSeconds: int(m.pollInterval() / time.Second),
		State:               m.st.state,
		LastAttemptAt:       copyTime(m.st.lastAttempt),
		LastSuccessAt:       copyTime(m.st.lastSuccess),
		LastDegree:          copyFloat(m.st.lastDegree),
		LastError:           copyError(m.st.lastError),
		NextPollAt:          copyTime(m.st.nextPoll),
		ConsecutiveFailures: m.st.failures,
	}
}

// StartedAt 返回循环开始时间, 尚未开始时为零值.
func (m *Monitor) StartedAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.startedAt
}

// SetStartedAt 设置循环开始时间, 用于进程接续或测试.
func (m *Monitor) SetStartedAt(t time.Time) {
	m.mu.Lock()
	m.st.startedAt = t
	m.mu.Unlock()
}

func (m *Monitor) wait(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-m.wake:
			return true
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-m.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (m *Monitor) transition(next string) {
	m.mu.Lock()
	prev := m.st.state
	m.st.state = next
	m.mu.Unlock()
	if prev != next {
		m.log.Info("采集状态变化", "from", prev, "to", next)
	}
}

func (m *Monitor) setAttempt(t time.Time) {
	m.mu.Lock()
	m.st.lastAttempt = &t
	m.mu.Unlock()
}

func (m *Monitor) planNext(delay time.Duration) {
	next := m.now().Add(delay)
	m.mu.Lock()
	m.st.nextPoll = &next
	m.mu.Unlock()
}

func (m *Monitor) setLastError(code string, err error) {
	info := &protocol.ErrorInfo{Code: code, Message: truncateMessage(err.Error())}
	m.mu.Lock()
	m.st.lastError = info
	m.mu.Unlock()
}

func (m *Monitor) pollInterval() time.Duration {
	if d := m.cfg.PollInterval.Std(); d > 0 {
		return d
	}
	return DefaultPollInterval
}

func (m *Monitor) threshold() float64 {
	if m.cfg.DegreeChangeThreshold > 0 {
		return m.cfg.DegreeChangeThreshold
	}
	return DefaultThreshold
}

func (m *Monitor) initialDelay() time.Duration {
	return time.Duration(rand.Int64N(int64(firstDelayMax)))
}

func (m *Monitor) sessionDelay() time.Duration {
	if interval := m.pollInterval(); interval > SessionProbeInterval {
		return interval
	}
	return SessionProbeInterval
}

// backoff 从 poll_interval 开始按失败次数翻倍, 上限 MaxBackoff.
func (m *Monitor) backoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	delay := m.pollInterval()
	for i := 1; i < failures && delay < MaxBackoff; i++ {
		delay *= 2
	}
	if delay > MaxBackoff {
		delay = MaxBackoff
	}
	return delay
}

func jitterAround(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return time.Duration(float64(base) * (0.9 + rand.Float64()*0.2))
}

func truncateMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= maxMessageRunes {
		return message
	}
	return string(runes[:maxMessageRunes]) + "..."
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyError(value *protocol.ErrorInfo) *protocol.ErrorInfo {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
