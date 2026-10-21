package localapi

import (
	"io"
	"net/http"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request, rid string) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: s.opts.Version,
		PID:     s.pid,
	})
}

type localStatus struct {
	Version             string     `json:"version"`
	UptimeSeconds       int64      `json:"uptime_seconds"`
	Health              string     `json:"health"`
	HealthDetail        *string    `json:"health_detail"`
	LastCheckAt         *time.Time `json:"last_check_at"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	UpstreamBase        string     `json:"upstream_base"`
}

type remoteStatus struct {
	Version       string                 `json:"version"`
	ServerTime    time.Time              `json:"server_time"`
	UptimeSeconds int64                  `json:"uptime_seconds"`
	ActiveRoom    *protocol.RoomSummary  `json:"active_room"`
	Session       protocol.SessionInfo   `json:"session"`
	Monitor       protocol.MonitorStatus `json:"monitor"`
	Storage       protocol.StorageStats  `json:"storage"`
}

type statusResponse struct {
	Local  localStatus   `json:"local"`
	Remote *remoteStatus `json:"remote"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, rid string) {
	snap := s.tracker.snapshot()
	if snap.Health == HealthStarting {
		// 首次访问时触发一次后台轮询, 不阻塞页面.
		s.tracker.requestRefresh()
	}
	cfg := s.currentConfig()
	uptime := int64(0)
	if !s.startedAt.IsZero() {
		uptime = int64(s.now().Sub(s.startedAt).Seconds())
	}
	out := statusResponse{
		Local: localStatus{
			Version:             s.opts.Version,
			UptimeSeconds:       uptime,
			Health:              snap.Health,
			LastCheckAt:         snap.LastCheckAt,
			ConsecutiveFailures: snap.Failures,
			UpstreamBase:        cfg.Server.BaseURL,
		},
	}
	if snap.Detail != "" {
		detail := snap.Detail
		out.Local.HealthDetail = &detail
	}
	if snap.Remote != nil {
		out.Remote = &remoteStatus{
			Version:       snap.Remote.Version,
			ServerTime:    snap.Remote.ServerTime,
			UptimeSeconds: snap.Remote.UptimeSecond,
			ActiveRoom:    snap.Remote.ActiveRoom,
			Session:       snap.Remote.Session,
			Monitor:       snap.Remote.Monitor,
			Storage:       snap.Remote.Storage,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request, rid string) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	io.Copy(io.Discard, r.Body)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting_down"})
	time.AfterFunc(shutdownDelay, func() {
		s.log.Info("收到本地关闭请求")
		s.Stop()
	})
}
