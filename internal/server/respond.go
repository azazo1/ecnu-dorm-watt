package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// jsonContentType 是 JSON 响应的 Content-Type.
const jsonContentType = "application/json"

// writeJSON 写出 JSON 响应.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		s.logger.Error("编码 JSON 响应失败", "path", r.URL.Path, "error", err)
		s.writeProblem(w, r, http.StatusInternalServerError, protocol.CodeInternal, "", "序列化响应失败")
		return
	}
	h := w.Header()
	h.Set("Content-Type", jsonContentType)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		s.logger.Debug("写出响应失败", "path", r.URL.Path, "error", err)
	}
}

// writeNoContent 写出 204 响应.
func (s *Server) writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON 严格解码请求体: 拒绝未知字段与多余内容, 并按上限截断.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || !isJSONMediaType(mediaType) {
			s.writeProblem(w, r, http.StatusUnsupportedMediaType, protocol.CodeUnsupportedMedia, "", "请求体必须是 application/json")
			return false
		}
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		s.writeDecodeProblem(w, r, err)
		return false
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		s.writeProblem(w, r, http.StatusBadRequest, protocol.CodeInvalidRequest, "", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

// writeDecodeProblem 把 JSON 解码错误映射为 problem 响应.
func (s *Server) writeDecodeProblem(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		s.writeProblem(w, r, http.StatusRequestEntityTooLarge, protocol.CodePayloadTooLarge, "", "请求体超过大小限制")
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		s.writeProblem(w, r, http.StatusBadRequest, protocol.CodeInvalidRequest, "", "请求体不是合法的 JSON 对象")
	case strings.Contains(err.Error(), "unknown field"):
		s.writeProblem(w, r, http.StatusBadRequest, protocol.CodeInvalidRequest, "", "请求体包含未知字段")
	default:
		s.writeProblem(w, r, http.StatusBadRequest, protocol.CodeInvalidRequest, "", "请求体不是合法的 JSON 对象")
	}
}

// isJSONMediaType 判断媒体类型是否为 JSON.
func isJSONMediaType(mediaType string) bool {
	if mediaType == "application/json" {
		return true
	}
	return strings.HasSuffix(mediaType, "+json")
}

// queryValue 取出查询参数的原值.
func queryValue(r *http.Request, name string) (string, bool) {
	values, ok := r.URL.Query()[name]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// parseQueryBool 解析布尔查询参数, present 表示参数是否出现.
func parseQueryBool(r *http.Request, name string) (value bool, present bool, err error) {
	raw, ok := queryValue(r, name)
	if !ok || raw == "" {
		return false, false, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true, nil
	case "0", "false", "no", "off":
		return false, true, nil
	default:
		return false, true, invalidRequestf("%s 必须是布尔值", name)
	}
}

// parseQueryInt64 解析整数查询参数.
func parseQueryInt64(r *http.Request, name string) (value int64, present bool, err error) {
	raw, ok := queryValue(r, name)
	if !ok || raw == "" {
		return 0, false, nil
	}
	v, convErr := strconv.ParseInt(raw, 10, 64)
	if convErr != nil {
		return 0, true, invalidRequestf("%s 必须是整数", name)
	}
	return v, true, nil
}

// parseQueryTime 解析带时区偏移的 RFC3339 查询参数.
func parseQueryTime(r *http.Request, name string) (*time.Time, error) {
	raw, ok := queryValue(r, name)
	if !ok || raw == "" {
		return nil, nil
	}
	if strings.TrimSpace(raw) != raw {
		return nil, invalidRequestf("%s 不能包含空白字符", name)
	}
	t, err := protocol.ParseTime(raw)
	if err != nil {
		return nil, validationFailedf("%s 必须是带时区偏移的 RFC3339 时间", name)
	}
	return &t, nil
}

// roomListResponse 是房间列表响应.
type roomListResponse struct {
	Rooms []protocol.Room `json:"rooms"`
}

// archiveListResponse 是归档列表响应.
type archiveListResponse struct {
	Archives []protocol.ArchiveMeta `json:"archives"`
}
