package localapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// newRequestID 生成本次请求的短 id.
func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf[:])
}

// writeProblem 输出 problem+json 错误.
func writeProblem(w http.ResponseWriter, rid string, status int, code, title, detail string) {
	p := protocol.Problem{
		Type:      "about:blank",
		Title:     title,
		Status:    status,
		Code:      code,
		Detail:    detail,
		RequestID: rid,
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Debug("写入 problem 响应失败", "request_id", rid, "error", err)
	}
}

// writeJSON 输出 JSON 响应.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("写入 JSON 响应失败", "error", err)
	}
}
