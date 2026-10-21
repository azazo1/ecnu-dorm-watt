// Package server 实现 dormwatt 服务端的 HTTP API.
//
// 本包只做协议转换: 持久化通过 store.DB 完成, 实时电量通过 DegreeSource 查询,
// ECNU 目录数据通过 CatalogSource 读取, 采集状态通过 MonitorState 观察.
// 本包不直接依赖 ecnu 与 monitor 包, 以便两者可以独立演进.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

const (
	defaultMaxBodyBytes = 64 * 1024
	defaultTimezone     = "Asia/Shanghai"
)

// DegreeSource 提供实时电量查询.
type DegreeSource interface {
	// Query 查询指定房间的剩余电量, session 为当前 ECNU 会话.
	Query(ctx context.Context, room store.Room, session store.Session) (float64, error)
}

// MonitorState 暴露采集循环的最近状态.
type MonitorState interface {
	// Status 返回最近一次采集状态, 不触发上游查询.
	Status() protocol.MonitorStatus
	// Wake 立即唤醒采集循环.
	Wake()
}

// RoomInfo 是房间名称解析结果.
type RoomInfo struct {
	Area     protocol.Area        `json:"area"`
	District protocol.District    `json:"district"`
	Building protocol.Building    `json:"building"`
	Floor    protocol.Floor       `json:"floor"`
	Room     protocol.CatalogRoom `json:"room"`
}

// CatalogSource 提供 ECNU 目录数据, 缓存由实现方负责.
type CatalogSource interface {
	Districts(ctx context.Context) (protocol.Districts, error)
	Buildings(ctx context.Context, areaID, districtID string) (protocol.Buildings, error)
	Floors(ctx context.Context, areaID, districtID, buildingID string) (protocol.Floors, error)
	Rooms(ctx context.Context, areaID, districtID, buildingID, floorID string) (protocol.CatalogRooms, error)
	RoomInfo(ctx context.Context, room store.Room) (RoomInfo, error)
}

// CodedError 提供稳定的错误码, 用于把上游错误映射为 problem code.
type CodedError interface {
	error
	Code() string
}

// Deps 是构造 Server 所需的依赖.
type Deps struct {
	// DB 是存储层, 必须非空.
	DB store.DB
	// Config 是服务端配置.
	Config config.ServerConfig
	// Logger 是日志器, 为空时使用 slog.Default.
	Logger *slog.Logger
	// Version 是服务版本号.
	Version string
	// StartedAt 是进程启动时间, 为零值时取构造时刻.
	StartedAt time.Time
	// StaticTokens 是配置中声明的静态 token.
	StaticTokens []config.StaticToken
	// Degree 提供实时电量查询, 为空时 /v1/degree 返回 503.
	Degree DegreeSource
	// Catalog 提供 ECNU 目录数据, 为空时目录接口返回 503.
	Catalog CatalogSource
	// Monitor 提供采集状态, 为空时 /v1/status 中的 monitor 使用默认值.
	Monitor MonitorState
}

// Server 持有 API 的全部依赖, 由 New 构造.
type Server struct {
	db        store.DB
	cfg       config.ServerConfig
	logger    *slog.Logger
	version   string
	startedAt time.Time
	loc       *time.Location

	degree  DegreeSource
	catalog CatalogSource
	monitor MonitorState

	staticTokens []staticToken
	trusted      []*net.IPNet

	maxBodyBytes int64

	failedAuth *limiter
	requests   *limiter
	upstream   *limiter

	tokenTouch *touchCache

	live    *liveQuery
	liveTTL time.Duration

	shuttingDown atomic.Bool

	handler http.Handler
}

// New 构造 Server 并完成路由挂载.
func New(deps Deps) *Server {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cfg := deps.Config
	if cfg.Server.MaxBodyBytes <= 0 {
		cfg.Server.MaxBodyBytes = defaultMaxBodyBytes
	}
	startedAt := deps.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	s := &Server{
		db:           deps.DB,
		cfg:          cfg,
		logger:       logger.With("component", "http"),
		version:      deps.Version,
		startedAt:    startedAt,
		loc:          loadLocation(cfg.UI.Timezone, logger),
		degree:       deps.Degree,
		catalog:      deps.Catalog,
		monitor:      deps.Monitor,
		maxBodyBytes: cfg.Server.MaxBodyBytes,
		staticTokens: buildStaticTokens(deps.StaticTokens),
		trusted:      parseTrustedProxies(cfg.Auth.TrustedProxies, logger),
		failedAuth:   newLimiter(cfg.Auth.FailedAuthPerMinute, cfg.Auth.FailedAuthPerMinute),
		requests:     newLimiter(cfg.Auth.RequestsPerMinute, 60),
		upstream:     newLimiter(cfg.Auth.UpstreamRequestsPerMin, cfg.Auth.UpstreamRequestsPerMin),
		tokenTouch:   newTouchCache(time.Minute),
		live:         newLiveQuery(),
		liveTTL:      liveQueryTTL(cfg.ECNU.LiveQueryCache.Std()),
	}
	s.handler = s.buildHandler()
	return s
}

// Handler 返回 HTTP 处理器.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// MarkShuttingDown 标记服务正在关闭, 之后 /v1/health 返回 503.
func (s *Server) MarkShuttingDown() {
	s.shuttingDown.Store(true)
}

// buildHandler 组装中间件链.
//
// 顺序自外向内: 请求 id, 请求日志, gzip, panic 恢复, 请求体上限, 路由.
func (s *Server) buildHandler() http.Handler {
	var h http.Handler = s.apiHandler()
	h = s.withBodyLimit(h)
	h = s.withRecover(h)
	h = s.withGzip(h)
	h = s.withLogging(h)
	h = s.withRequestID(h)
	return h
}

// apiHandler 只接受 /v1 路由, 并拒绝需要路径规范化的请求.
func (s *Server) apiHandler() http.Handler {
	mux := s.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" && !strings.HasPrefix(r.URL.Path, "/v1/") {
			s.handleNotFound(w, r)
			return
		}
		if path.Clean(r.URL.Path) != r.URL.Path {
			s.handleNotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// handleNotFound 写出统一的 404 problem.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "请求的路径不存在")
}

// loadLocation 读取展示时区, 失败时退回默认时区与本地时区.
func loadLocation(name string, logger *slog.Logger) *time.Location {
	for _, candidate := range []string{name, defaultTimezone} {
		if candidate == "" {
			continue
		}
		loc, err := time.LoadLocation(candidate)
		if err == nil {
			return loc
		}
		logger.Warn("无法加载时区", "timezone", candidate, "error", err)
	}
	return time.Local
}

// exportTime 把时间转换到展示时区并截断到秒.
func (s *Server) exportTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(s.loc).Truncate(time.Second)
}

// exportTimePtr 是 exportTime 的指针版本, 输入为空时返回 nil.
func (s *Server) exportTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := s.exportTime(*t)
	return &v
}
