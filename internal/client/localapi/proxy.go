package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// proxyPath 返回远端路径, 路径是否存在以及方法是否允许.
func proxyPath(r *http.Request) (string, bool, bool) {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/") {
		return "", false, false
	}
	rest := strings.TrimPrefix(p, "/api/")
	seg := strings.Split(rest, "/")
	allowed := func(methods ...string) bool {
		for _, m := range methods {
			if r.Method == m {
				return true
			}
		}
		return false
	}
	remote := func() string {
		escaped := r.URL.EscapedPath()
		return "/v1/" + strings.TrimPrefix(escaped, "/api/")
	}
	single := func(methods ...string) (string, bool, bool) {
		return remote(), true, allowed(methods...)
	}

	switch {
	case len(seg) == 1 && seg[0] == "degree":
		return single(http.MethodGet)
	case len(seg) == 1 && seg[0] == "records":
		return single(http.MethodGet)
	case len(seg) == 1 && seg[0] == "rooms":
		return single(http.MethodGet, http.MethodPost)
	case len(seg) == 2 && seg[0] == "rooms" && seg[1] == "active":
		return single(http.MethodGet, http.MethodDelete)
	case len(seg) == 1 && seg[0] == "session":
		return single(http.MethodGet, http.MethodPut, http.MethodDelete)
	case len(seg) == 1 && seg[0] == "archives":
		return single(http.MethodGet, http.MethodPost)
	case len(seg) == 2 && seg[0] == "ecnu":
		switch seg[1] {
		case "districts", "buildings", "floors", "rooms":
			return single(http.MethodGet)
		}
		return "", false, false
	case len(seg) == 2 && seg[0] == "archives" && seg[1] != "":
		return single(http.MethodGet, http.MethodDelete)
	case len(seg) == 3 && seg[0] == "rooms":
		if _, err := strconv.ParseInt(seg[1], 10, 64); err != nil {
			return "", false, false
		}
		switch seg[2] {
		case "active":
			return single(http.MethodPut)
		case "info":
			return single(http.MethodGet)
		}
		return "", false, false
	}
	return "", false, false
}

// handleProxy 把 /api/* 透传到远端 /v1/*.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request, rid string) {
	remotePath, exists, methodOK := proxyPath(r)
	if !exists {
		writeProblem(w, rid, http.StatusNotFound, protocol.CodeNotFound, "本地路径不存在", "没有匹配的本地 API 路径")
		return
	}
	if !methodOK {
		methodNotAllowed(w, r, rid)
		return
	}

	var body []byte
	if !isSafeMethod(r.Method) {
		limited := http.MaxBytesReader(w, r.Body, 64<<10)
		data, err := io.ReadAll(limited)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeProblem(w, rid, http.StatusRequestEntityTooLarge, protocol.CodePayloadTooLarge, "请求体过大", "请求体不能超过 64 KiB")
				return
			}
			writeProblem(w, rid, http.StatusBadRequest, protocol.CodeInvalidRequest, "请求体读取失败", err.Error())
			return
		}
		body = data
	}

	client := s.currentClient()
	if client == nil {
		writeProblem(w, rid, http.StatusBadGateway, protocol.CodeUpstreamUnreachable, "上游不可用", "上游客户端未初始化")
		return
	}
	resp, err := client.DoRaw(r.Context(), r.Method, remotePath, r.URL.Query(), body, "application/json")
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		writeProblem(w, rid, http.StatusBadGateway, protocol.CodeUpstreamUnreachable, "上游不可达", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		s.writeProxiedError(w, resp, rid)
		return
	}
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		s.log.Debug("透传响应体中断", "request_id", rid, "error", err)
	}
}

// writeProxiedError 透传 problem+json, 并写入本地 request_id.
func (s *Server) writeProxiedError(w http.ResponseWriter, resp *http.Response, rid string) {
	copyResponseHeaders(w.Header(), resp.Header)
	ctype := resp.Header.Get("Content-Type")
	if !strings.Contains(ctype, "problem+json") {
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		w.WriteHeader(resp.StatusCode)
		return
	}
	var p protocol.Problem
	if json.Unmarshal(raw, &p) != nil {
		w.WriteHeader(resp.StatusCode)
		w.Write(raw)
		return
	}
	if p.RequestID != "" {
		w.Header().Set("X-Upstream-Request-Id", p.RequestID)
	}
	p.RequestID = rid
	if p.Type == "" {
		p.Type = "about:blank"
	}
	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(p)
}

func copyResponseHeaders(dst, src http.Header) {
	for _, key := range []string{"Content-Type", "Content-Disposition", "Cache-Control", "ETag", "Last-Modified"} {
		if v := src.Get(key); v != "" {
			dst.Set(key, v)
		}
	}
}
