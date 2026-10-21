package server

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// healthResponse 是 /v1/health 的响应.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// handleHealth 处理存活检查, 不查询数据库与上游.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.shuttingDown.Load() {
		s.writeProblem(w, r, http.StatusServiceUnavailable, protocol.CodeUnavailable, "", "服务正在关闭")
		return
	}
	s.writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok", Version: s.version})
}

// handleWhoAmI 返回当前 token 的信息.
func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	info, ok := authFrom(r.Context())
	if !ok {
		s.writeProblem(w, r, http.StatusUnauthorized, protocol.CodeUnauthorized, "", "缺少或无效的 Bearer token")
		return
	}
	scopes := normalizeScopes(info.scopes)
	if scopes == nil {
		scopes = []string{}
	}
	s.writeJSON(w, r, http.StatusOK, protocol.WhoAmI{Name: info.name, Scopes: scopes})
}

// handleStatus 汇总服务状态, 不触发上游查询.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()

	var active *protocol.RoomSummary
	room, err := s.db.ActiveRoom(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.writeStorageProblem(w, r, err)
		return
	}
	if room != nil {
		active = &protocol.RoomSummary{
			ID:      room.ID,
			RoomNo:  room.RoomNo,
			Elcarea: room.Elcarea,
			Elcbuis: room.Elcbuis,
		}
	}

	session, err := s.loadSessionInfo(ctx)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}

	monitor := s.monitorStatus()
	stats, err := s.db.Stats(ctx)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}

	uptime := int64(now.Sub(s.startedAt).Seconds())
	if uptime < 0 {
		uptime = 0
	}
	s.writeJSON(w, r, http.StatusOK, protocol.Status{
		Version:      s.version,
		ServerTime:   s.exportTime(now),
		UptimeSecond: uptime,
		BasePath:     s.BasePath(),
		ActiveRoom:   active,
		Session:      session,
		Monitor:      monitor,
		Storage:      protocol.StorageStats{Samples: stats.Samples, Archives: stats.Archives},
	})
}

// loadSessionInfo 读取会话状态, 不返回会话值.
func (s *Server) loadSessionInfo(ctx context.Context) (protocol.SessionInfo, error) {
	session, err := s.db.GetSession(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.SessionInfo{State: "missing"}, nil
		}
		return protocol.SessionInfo{}, err
	}
	info := protocol.SessionInfo{State: session.State}
	if info.State == "" {
		info.State = "unknown"
	}
	if !session.UpdatedAt.IsZero() {
		updated := s.exportTime(session.UpdatedAt)
		info.UpdatedAt = &updated
	}
	info.LastCheckedAt = s.exportTimePtr(session.LastCheckedAt)
	return info, nil
}

// monitorStatus 读取采集状态, 并把时间转换到展示时区.
func (s *Server) monitorStatus() protocol.MonitorStatus {
	var status protocol.MonitorStatus
	if s.monitor != nil {
		status = s.monitor.Status()
	}
	if status.PollIntervalSeconds <= 0 {
		status.PollIntervalSeconds = int(s.cfg.ECNU.PollInterval.Std().Seconds())
	}
	if status.State == "" {
		status.State = "unknown"
	}
	status.LastAttemptAt = s.exportTimePtr(status.LastAttemptAt)
	status.LastSuccessAt = s.exportTimePtr(status.LastSuccessAt)
	status.NextPollAt = s.exportTimePtr(status.NextPollAt)
	if status.LastDegree != nil {
		v := roundDegree(*status.LastDegree)
		status.LastDegree = &v
	}
	return status
}

// roundDegree 把电量保留两位小数.
func roundDegree(v float64) float64 {
	return math.Round(v*100) / 100
}

// handleDegree 查询当前电量.
func (s *Server) handleDegree(w http.ResponseWriter, r *http.Request) {
	fresh, _, err := parseQueryBool(r, "fresh")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	ctx := r.Context()

	room, ok := s.requireActiveRoom(w, r)
	if !ok {
		return
	}
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if s.degree == nil {
		s.writeProblem(w, r, http.StatusServiceUnavailable, protocol.CodeUnavailable, "", "上游查询尚未配置")
		return
	}
	now := time.Now()
	if !fresh {
		if entry, ok := s.live.get(room.ID, now); ok {
			until := s.exportTime(entry.until)
			s.writeJSON(w, r, http.StatusOK, protocol.Degree{
				Degree:      roundDegree(entry.degree),
				QueriedAt:   s.exportTime(entry.queriedAt),
				Source:      "cache",
				CachedUntil: &until,
			})
			return
		}
	}
	if fresh && !s.takeUpstream(w, r) {
		return
	}
	degree, err := s.degree.Query(ctx, *room, *session)
	if err != nil {
		s.writeUpstreamProblem(w, r, err)
		return
	}
	entry := liveEntry{degree: degree, queriedAt: now, until: now.Add(s.liveTTL)}
	s.live.put(room.ID, entry, now)
	until := s.exportTime(entry.until)
	s.writeJSON(w, r, http.StatusOK, protocol.Degree{
		Degree:      roundDegree(degree),
		QueriedAt:   s.exportTime(now),
		Source:      "upstream",
		CachedUntil: &until,
	})
}

// requireSession 读取可用会话, 缺失或已失效时写出 502 并返回 false.
func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) (*store.Session, bool) {
	session, err := s.db.GetSession(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusBadGateway, protocol.CodeECNUNotLoggedIn, "", "尚未上传 ECNU 会话")
			return nil, false
		}
		s.writeStorageProblem(w, r, err)
		return nil, false
	}
	if session.State == "invalid" {
		s.writeProblem(w, r, http.StatusBadGateway, protocol.CodeECNUNotLoggedIn, "", "ECNU 会话已失效, 请重新上传")
		return nil, false
	}
	return session, true
}

// requireActiveRoom 读取活动房间, 不存在时写出 409 并返回 false.
func (s *Server) requireActiveRoom(w http.ResponseWriter, r *http.Request) (*store.Room, bool) {
	room, err := s.db.ActiveRoom(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusConflict, protocol.CodeRoomNotConfigured, "", "当前没有活动房间, 请先设置房间")
			return nil, false
		}
		s.writeStorageProblem(w, r, err)
		return nil, false
	}
	return room, true
}

// queryRoom 解析查询参数中的 room_id, 缺省时使用活动房间.
func (s *Server) queryRoom(w http.ResponseWriter, r *http.Request, name string) (*store.Room, bool) {
	raw, present := queryValue(r, name)
	if !present || raw == "" {
		return s.requireActiveRoom(w, r)
	}
	id, _, err := parseQueryInt64(r, name)
	if err != nil {
		s.writeParamProblem(w, r, err)
		return nil, false
	}
	if id <= 0 {
		s.writeParamProblem(w, r, invalidRequestf("%s 必须是正整数", name))
		return nil, false
	}
	room, err := s.db.GetRoom(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "房间不存在")
			return nil, false
		}
		s.writeStorageProblem(w, r, err)
		return nil, false
	}
	return room, true
}

// exportRoom 把存储层房间转换为响应结构.
func (s *Server) exportRoom(ctx context.Context, room store.Room, active bool) (protocol.Room, error) {
	out := protocol.Room{
		ID:        room.ID,
		RoomNo:    room.RoomNo,
		Elcarea:   room.Elcarea,
		Elcbuis:   room.Elcbuis,
		Active:    active,
		CreatedAt: s.exportTime(room.CreatedAt),
	}
	stats, err := s.db.RoomStats(ctx, room.ID)
	if err != nil {
		return protocol.Room{}, err
	}
	out.SampleCount = stats.SampleCount
	out.LastSampleAt = s.exportTimePtr(stats.LastSampleAt)
	return out, nil
}
