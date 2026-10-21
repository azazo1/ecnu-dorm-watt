package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// problemTitles 是错误码对应的稳定标题.
var problemTitles = map[string]string{
	protocol.CodeInvalidRequest:    "请求不合法",
	protocol.CodeUnauthorized:      "需要认证",
	protocol.CodeForbidden:         "权限不足",
	protocol.CodeNotFound:          "资源不存在",
	protocol.CodeMethodNotAllowed:  "方法不允许",
	protocol.CodeRoomNotConfigured: "未配置房间",
	protocol.CodeRoomExists:        "房间已存在",
	protocol.CodeArchiveExists:     "归档已存在",
	protocol.CodeConflict:          "状态冲突",
	protocol.CodePayloadTooLarge:   "请求体过大",
	protocol.CodeUnsupportedMedia:  "不支持的媒体类型",
	protocol.CodeValidationFailed:  "字段校验失败",
	protocol.CodeArchiveEmpty:      "归档范围没有样本",
	protocol.CodeRecordsTooMany:    "记录数超出上限",
	protocol.CodeRateLimited:       "请求过于频繁",
	protocol.CodeStorageError:      "存储错误",
	protocol.CodeInternal:          "内部错误",
	protocol.CodeECNUNotLoggedIn:   "ECNU 会话不可用",
	protocol.CodeECNUUpstream:      "上游请求失败",
	protocol.CodeECNUNoDegree:      "上游响应缺少电量",
	protocol.CodeCatalogIncomplete: "上游目录数据不完整",
	protocol.CodeUnavailable:       "服务不可用",
}

// problemTitle 返回错误码的标题, 未知错误码使用兜底标题.
func problemTitle(code string) string {
	if title, ok := problemTitles[code]; ok {
		return title
	}
	return "请求失败"
}

// writeProblem 写出 RFC 9457 problem 响应.
//
// title 为空时按 code 取默认标题, detail 可以为空.
func (s *Server) writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	if title == "" {
		title = problemTitle(code)
	}
	p := protocol.Problem{
		Type:      "about:blank",
		Title:     title,
		Status:    status,
		Code:      code,
		Detail:    detail,
		RequestID: requestIDFrom(r.Context()),
	}
	body, err := json.Marshal(p)
	if err != nil {
		// 结构化错误信息本身无法编码时退化为纯文本, 避免再次进入 problem 流程.
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if status == http.StatusUnauthorized {
		h.Set("WWW-Authenticate", `Bearer realm="dormwatt"`)
	}
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		s.logger.Debug("写出错误响应失败", "path", r.URL.Path, "error", err)
	}
}

// writeParamProblem 写出参数类错误.
func (s *Server) writeParamProblem(w http.ResponseWriter, r *http.Request, err error) {
	var pe *paramError
	if errors.As(err, &pe) {
		s.writeProblem(w, r, pe.status, pe.code, "", pe.detail)
		return
	}
	s.logger.Error("未分类的参数错误", "path", r.URL.Path, "error", err)
	s.writeProblem(w, r, http.StatusInternalServerError, protocol.CodeInternal, "", "服务器内部错误")
}

// writeStorageProblem 记录并写出存储层错误.
func (s *Server) writeStorageProblem(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("存储操作失败", "path", r.URL.Path, "request_id", requestIDFrom(r.Context()), "error", err)
	s.writeProblem(w, r, http.StatusInternalServerError, protocol.CodeStorageError, "", "数据库操作失败")
}

// writeRateLimited 写出 429 响应, 并附带 Retry-After.
func (s *Server) writeRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration, detail string) {
	seconds := int(retryAfter / time.Second)
	if time.Duration(seconds)*time.Second < retryAfter {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	s.writeProblem(w, r, http.StatusTooManyRequests, protocol.CodeRateLimited, "", detail)
}

// writeUpstreamProblem 把上游错误映射为 problem 响应.
//
// 上游错误如果实现 CodedError, 则采用其错误码; 否则统一为 ecnu_upstream_error.
func (s *Server) writeUpstreamProblem(w http.ResponseWriter, r *http.Request, err error) {
	code := protocol.CodeECNUUpstream
	var coded CodedError
	if errors.As(err, &coded) {
		switch coded.Code() {
		case protocol.CodeECNUNotLoggedIn,
			protocol.CodeECNUUpstream,
			protocol.CodeECNUNoDegree,
			protocol.CodeCatalogIncomplete,
			protocol.CodeUnavailable:
			code = coded.Code()
		}
	}
	s.logger.Warn("上游请求失败", "path", r.URL.Path, "request_id", requestIDFrom(r.Context()), "code", code, "error", err)
	s.writeProblem(w, r, statusForCode(code), code, "", "上游请求失败, 请稍后重试")
}

// statusForCode 返回错误码对应的 HTTP 状态码.
func statusForCode(code string) int {
	switch code {
	case protocol.CodeECNUNotLoggedIn,
		protocol.CodeECNUUpstream,
		protocol.CodeECNUNoDegree,
		protocol.CodeCatalogIncomplete:
		return http.StatusBadGateway
	case protocol.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// requestIDFrom 从上下文取出请求 id.
func requestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return id
	}
	return ""
}

// paramError 表示查询参数或请求字段不合法.
type paramError struct {
	status int
	code   string
	detail string
}

func (e *paramError) Error() string { return e.detail }

// invalidRequestf 构造 400 invalid_request 错误.
func invalidRequestf(format string, args ...any) *paramError {
	return &paramError{
		status: http.StatusBadRequest,
		code:   protocol.CodeInvalidRequest,
		detail: fmt.Sprintf(format, args...),
	}
}

// validationFailedf 构造 422 validation_failed 错误.
func validationFailedf(format string, args ...any) *paramError {
	return &paramError{
		status: http.StatusUnprocessableEntity,
		code:   protocol.CodeValidationFailed,
		detail: fmt.Sprintf(format, args...),
	}
}

// mapStoreError 把存储错误转换为 problem 响应, 返回 true 表示已经写出响应.
func (s *Server) mapStoreError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "资源不存在")
		return true
	default:
		s.writeStorageProblem(w, r, err)
		return true
	}
}
