package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// ctxKey 是服务端内部上下文的键类型.
type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyAuth
	ctxKeyAudit
)

// requestIDPattern 是合法 X-Request-Id 的格式.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// requestAudit 收集一次请求的审计信息, 由认证中间件填充.
type requestAudit struct {
	tokenName string
}

// withRequestID 接收或生成请求 id, 写入响应头与上下文.
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if !requestIDPattern.MatchString(rid) {
			rid = newRequestID()
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// newRequestID 生成 16 个十六进制字符的随机请求 id.
func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000")
	}
	return hex.EncodeToString(buf[:])
}

// statusRecorder 记录响应状态码.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.status = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}

// Flush 透传给底层 ResponseWriter.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap 支持 http.ResponseController.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// withLogging 记录请求日志, 不记录请求体与凭据.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		audit := &requestAudit{}
		ctx := context.WithValue(r.Context(), ctxKeyAudit, audit)
		next.ServeHTTP(rec, r.WithContext(ctx))

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(ctx),
			"remote_ip", s.clientIP(r),
		}
		if audit.tokenName != "" {
			attrs = append(attrs, "token_name", audit.tokenName)
		}
		s.logger.Info("http request", attrs...)
	})
}

// withRecover 捕获 panic, 记录堆栈并返回 500 problem.
func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			s.logger.Error("处理请求时发生 panic",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", requestIDFrom(r.Context()),
				"panic", v,
				"stack", string(debug.Stack()),
			)
			if responseStarted(w) {
				// 响应已经开始写出, 无法再改动状态码.
				return
			}
			s.writeProblem(w, r, http.StatusInternalServerError, protocol.CodeInternal, "", "服务器内部错误")
		}()
		next.ServeHTTP(w, r)
	})
}

// responseStarted 判断响应是否已经写出.
func responseStarted(w http.ResponseWriter) bool {
	for {
		switch v := w.(type) {
		case *statusRecorder:
			if v.written {
				return true
			}
			w = v.ResponseWriter
		case *gzipWriter:
			if v.written {
				return true
			}
			w = v.ResponseWriter
		case interface{ Unwrap() http.ResponseWriter }:
			w = v.Unwrap()
		default:
			return false
		}
	}
}

// withBodyLimit 给请求体加上大小上限.
func (s *Server) withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// gzipThreshold 是启用 gzip 的最小响应体大小.
const gzipThreshold = 1024

// withGzip 对客户端声明支持的 JSON 响应启用 gzip 压缩.
//
// 只有 JSON 类响应会被压缩, CSV 流式下载保持原样; 响应体小于阈值时也保持原样.
func (s *Server) withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gz := &gzipWriter{ResponseWriter: w}
		defer gz.finish()
		next.ServeHTTP(gz, r)
	})
}

// acceptsGzip 判断客户端是否接受 gzip 编码.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		token := strings.TrimSpace(part)
		if i := strings.IndexByte(token, ';'); i >= 0 {
			q := strings.TrimSpace(token[i+1:])
			token = strings.TrimSpace(token[:i])
			if q == "q=0" || q == "q=0.0" || q == "q=0.00" || q == "q=0.000" {
				continue
			}
		}
		if strings.EqualFold(token, "gzip") {
			return true
		}
	}
	return false
}

// gzipWriter 在响应体超过阈值时才切换为 gzip 压缩.
type gzipWriter struct {
	http.ResponseWriter
	buf      bytes.Buffer
	gz       *gzip.Writer
	status   int
	written  bool
	compress bool
	skip     bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.written {
		g.ResponseWriter.WriteHeader(code)
		return
	}
	g.written = true
	g.status = code
	ct := g.Header().Get("Content-Type")
	g.skip = code == http.StatusNoContent || code == http.StatusNotModified || !isJSONContentType(ct)
	if g.skip {
		g.ResponseWriter.WriteHeader(code)
	}
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.written {
		g.WriteHeader(http.StatusOK)
	}
	if g.skip {
		return g.ResponseWriter.Write(p)
	}
	if g.compress {
		return g.gz.Write(p)
	}
	if g.buf.Len()+len(p) <= gzipThreshold {
		return g.buf.Write(p)
	}
	if err := g.startGzip(); err != nil {
		return 0, err
	}
	return g.gz.Write(p)
}

// startGzip 写出响应头并开始压缩, 之前缓冲的内容一并写出.
func (g *gzipWriter) startGzip() error {
	h := g.Header()
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
	h.Add("Vary", "Accept-Encoding")
	g.ResponseWriter.WriteHeader(g.status)
	gz := gzip.NewWriter(g.ResponseWriter)
	g.gz = gz
	g.compress = true
	if g.buf.Len() > 0 {
		if _, err := gz.Write(g.buf.Bytes()); err != nil {
			return err
		}
		g.buf.Reset()
	}
	return nil
}

// finish 结束响应: 未压缩的缓冲内容原样写出, 压缩流则关闭.
func (g *gzipWriter) finish() {
	if !g.written || g.skip {
		return
	}
	if g.compress {
		if err := g.gz.Close(); err != nil {
			return
		}
		return
	}
	g.ResponseWriter.WriteHeader(g.status)
	if g.buf.Len() > 0 {
		_, _ = g.ResponseWriter.Write(g.buf.Bytes())
	}
}

// Flush 满足流式输出.
func (g *gzipWriter) Flush() {
	if g.compress {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap 支持 http.ResponseController.
func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// isJSONContentType 判断响应类型是否需要压缩.
func isJSONContentType(ct string) bool {
	if ct == "" {
		return true
	}
	return strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "application/problem+json")
}

// clientIP 返回客户端地址.
//
// 只有对端命中 trusted_proxies 时才采信 X-Forwarded-For, 并从右向左取第一个
// 不可信地址.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if !s.isTrustedProxy(ip) {
		return ip.String()
	}
	var chain []net.IP
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(header, ",") {
			if v := net.ParseIP(strings.TrimSpace(part)); v != nil {
				chain = append(chain, v)
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if !s.isTrustedProxy(chain[i]) {
			return chain[i].String()
		}
	}
	if len(chain) > 0 {
		return chain[0].String()
	}
	return ip.String()
}

// isTrustedProxy 判断地址是否是可信托代理.
func (s *Server) isTrustedProxy(ip net.IP) bool {
	for _, network := range s.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// parseTrustedProxies 解析可信代理列表, 支持 CIDR 与单个地址.
func parseTrustedProxies(raw []string, logger *slog.Logger) []*net.IPNet {
	var networks []*net.IPNet
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			networks = append(networks, network)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			logger.Warn("忽略无法解析的可信代理", "value", entry)
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return networks
}
