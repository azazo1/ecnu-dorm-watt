package protocol

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	CodeInvalidRequest      = "invalid_request"
	CodeUnauthorized        = "unauthorized"
	CodeForbidden           = "forbidden"
	CodeNotFound            = "not_found"
	CodeMethodNotAllowed    = "method_not_allowed"
	CodeRoomNotConfigured   = "room_not_configured"
	CodeRoomExists          = "room_exists"
	CodeArchiveExists       = "archive_exists"
	CodeConflict            = "conflict"
	CodePayloadTooLarge     = "payload_too_large"
	CodeUnsupportedMedia    = "unsupported_media_type"
	CodeValidationFailed    = "validation_failed"
	CodeArchiveEmpty        = "archive_empty"
	CodeRecordsTooMany      = "records_too_many"
	CodeRateLimited         = "rate_limited"
	CodeStorageError        = "storage_error"
	CodeInternal            = "internal"
	CodeECNUNotLoggedIn     = "ecnu_not_logged_in"
	CodeECNUUpstream        = "ecnu_upstream_error"
	CodeECNUNoDegree        = "ecnu_no_degree"
	CodeCatalogIncomplete   = "catalog_incomplete"
	CodeUnavailable         = "unavailable"
	CodeUpstreamUnreachable = "upstream_unreachable"
)

type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type Room struct {
	ID           int64      `json:"id"`
	RoomNo       string     `json:"room_no"`
	Elcarea      int        `json:"elcarea"`
	Elcbuis      string     `json:"elcbuis"`
	Active       bool       `json:"active"`
	CreatedAt    time.Time  `json:"created_at"`
	SampleCount  int        `json:"sample_count"`
	LastSampleAt *time.Time `json:"last_sample_at,omitempty"`
}

type Sample struct {
	Time   time.Time `json:"time"`
	Degree float64   `json:"degree"`
}

type SessionInfo struct {
	State         string     `json:"state"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
}

type MonitorStatus struct {
	PollIntervalSeconds int        `json:"poll_interval_seconds"`
	State               string     `json:"state"`
	LastAttemptAt       *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastDegree          *float64   `json:"last_degree,omitempty"`
	LastError           *ErrorInfo `json:"last_error,omitempty"`
	NextPollAt          *time.Time `json:"next_poll_at,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type StorageStats struct {
	Samples  int `json:"samples"`
	Archives int `json:"archives"`
}

type Status struct {
	Version      string        `json:"version"`
	ServerTime   time.Time     `json:"server_time"`
	UptimeSecond int64         `json:"uptime_seconds"`
	ActiveRoom   *RoomSummary  `json:"active_room"`
	Session      SessionInfo   `json:"session"`
	Monitor      MonitorStatus `json:"monitor"`
	Storage      StorageStats  `json:"storage"`
}

type RoomSummary struct {
	ID      int64  `json:"id"`
	RoomNo  string `json:"room_no"`
	Elcarea int    `json:"elcarea"`
	Elcbuis string `json:"elcbuis"`
}

type Degree struct {
	Degree      float64    `json:"degree"`
	QueriedAt   time.Time  `json:"queried_at"`
	Source      string     `json:"source"`
	CachedUntil *time.Time `json:"cached_until,omitempty"`
}

type Records struct {
	RoomID  int64      `json:"room_id"`
	RoomNo  string     `json:"room_no"`
	From    *time.Time `json:"from"`
	To      *time.Time `json:"to"`
	Records []Sample   `json:"records"`
}

type ArchiveMeta struct {
	Name       string    `json:"name"`
	RoomID     int64     `json:"room_id"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	RecordsNum int       `json:"records_num"`
	CreatedAt  time.Time `json:"created_at"`
}

type WhoAmI struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type Area struct {
	AreaID   string `json:"area_id"`
	AreaName string `json:"area_name"`
}

type District struct {
	DistrictID   string `json:"district_id"`
	DistrictName string `json:"district_name"`
}

type Building struct {
	BuildingID   string `json:"building_id"`
	BuildingName string `json:"building_name"`
}

type Floor struct {
	FloorID   string `json:"floor_id"`
	FloorName string `json:"floor_name"`
}

type CatalogRoom struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

type Districts struct {
	Areas     []Area     `json:"areas"`
	Districts []District `json:"districts"`
}

type Buildings struct {
	Buildings []Building `json:"buildings"`
}

type Floors struct {
	Floors []Floor `json:"floors"`
}

type CatalogRooms struct {
	Rooms []CatalogRoom `json:"rooms"`
}

// JoinBaseURL 在保留 base 路径的前提下追加 suffix.
func JoinBaseURL(base, suffix string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(suffix, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// ParseTime 解析带时区偏移的 RFC3339 时间.
func ParseTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("time must be RFC3339 with offset: %w", err)
	}
	return t, nil
}

// FormatTime 按给定时区输出 RFC3339, 最多三位小数.
func FormatTime(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02T15:04:05.999Z07:00")
}
