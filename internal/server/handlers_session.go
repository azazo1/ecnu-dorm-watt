package server

import (
	"net/http"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// sessionUploadLimit 是会话上传请求体的上限.
const sessionUploadLimit = 32 * 1024

// maxSessionFieldLength 是会话字段的字节上限.
const maxSessionFieldLength = 8192

// sessionRequest 是上传会话的请求.
type sessionRequest struct {
	JSESSIONID string `json:"jsession_id"`
	Cookie     string `json:"cookie"`
	CSRFToken  string `json:"x_csrf_token"`
}

// handleGetSession 返回会话状态, 不返回会话值.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	info, err := s.loadSessionInfo(r.Context())
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, info)
}

// handlePutSession 覆盖保存 ECNU 会话.
func (s *Server) handlePutSession(w http.ResponseWriter, r *http.Request) {
	limit := s.maxBodyBytes
	if limit > sessionUploadLimit {
		limit = sessionUploadLimit
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)

	var req sessionRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := validateSessionField("jsession_id", req.JSESSIONID); err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if err := validateSessionField("cookie", req.Cookie); err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if err := validateSessionField("x_csrf_token", req.CSRFToken); err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	session := store.Session{
		JSESSIONID: req.JSESSIONID,
		Cookie:     req.Cookie,
		CSRFToken:  req.CSRFToken,
		State:      "unknown",
		UpdatedAt:  time.Now(),
	}
	if err := s.db.SaveSession(r.Context(), session); err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	s.logger.Info("已更新 ECNU 会话", "request_id", requestIDFrom(r.Context()))
	s.writeNoContent(w)
}

// handleDeleteSession 清除 ECNU 会话.
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteSession(r.Context()); err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	s.logger.Info("已清除 ECNU 会话", "request_id", requestIDFrom(r.Context()))
	s.writeNoContent(w)
}

// validateSessionField 校验会话字段格式.
//
// 只检查长度与字符集, 不检查真实有效性, 也不回显字段值.
func validateSessionField(name, value string) error {
	if value == "" {
		return validationFailedf("%s 不能为空", name)
	}
	if len(value) > maxSessionFieldLength {
		return validationFailedf("%s 不能超过 %d 字节", name, maxSessionFieldLength)
	}
	if hasControlOrSpace(value) {
		return validationFailedf("%s 不能包含控制字符或空白字符", name)
	}
	return nil
}
