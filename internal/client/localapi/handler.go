package localapi

import (
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// Handler 返回本地服务路由.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.route)
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	rid := newRequestID()
	w.Header().Set("X-Request-Id", rid)
	setSecurityHeaders(w)

	if err := s.checkHost(r); err != nil {
		writeProblem(w, rid, http.StatusForbidden, "local_forbidden", "本地请求被拒绝", err.Error())
		return
	}
	if r.Method == http.MethodOptions {
		writeProblem(w, rid, http.StatusMethodNotAllowed, protocol.CodeMethodNotAllowed, "本地请求被拒绝", "OPTIONS 请求不被支持")
		return
	}
	if isAPIPath(r.URL.Path) {
		s.serveAPI(w, r, rid)
		return
	}
	s.serveStatic(w, r, rid)
}

func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request, rid string) {
	if !isSafeMethod(r.Method) {
		if err := checkJSONContentType(r); err != nil {
			writeProblem(w, rid, http.StatusForbidden, "local_forbidden", "本地请求被拒绝", err.Error())
			return
		}
		if r.Header.Get("X-Dormwatt-Client") != "1" {
			writeProblem(w, rid, http.StatusForbidden, "local_forbidden", "本地请求被拒绝", "缺少 X-Dormwatt-Client: 1")
			return
		}
		if err := checkOrigin(r); err != nil {
			writeProblem(w, rid, http.StatusForbidden, "local_forbidden", "本地请求被拒绝", err.Error())
			return
		}
	}
	if !s.authenticate(r) {
		writeProblem(w, rid, http.StatusUnauthorized, protocol.CodeUnauthorized, "本地会话无效", "缺少或错误的本地 token")
		return
	}

	switch r.URL.Path {
	case "/api/health":
		if !methodAllowed(w, r, rid, http.MethodGet) {
			return
		}
		s.handleHealth(w, r, rid)
		return
	case "/api/status":
		if !methodAllowed(w, r, rid, http.MethodGet) {
			return
		}
		s.handleStatus(w, r, rid)
		return
	case "/api/settings":
		switch r.Method {
		case http.MethodGet:
			s.handleGetSettings(w, r, rid)
		case http.MethodPut:
			s.handlePutSettings(w, r, rid)
		default:
			methodNotAllowed(w, r, rid)
		}
		return
	case "/api/shutdown":
		if !methodAllowed(w, r, rid, http.MethodPost) {
			return
		}
		s.handleShutdown(w, r, rid)
		return
	}
	s.handleProxy(w, r, rid)
}

// methodAllowed 检查方法是否匹配, 不匹配时写出 405.
func methodAllowed(w http.ResponseWriter, r *http.Request, rid string, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	methodNotAllowed(w, r, rid)
	return false
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request, rid string) {
	w.Header().Set("Allow", "GET, POST, PUT, DELETE")
	writeProblem(w, rid, http.StatusMethodNotAllowed, protocol.CodeMethodNotAllowed, "方法不被支持", r.Method+" 不被该路径支持")
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// checkHost 校验 Host 是否为回环地址与正确端口, 防止 DNS rebinding.
func (s *Server) checkHost(r *http.Request) error {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		return &hostError{host: r.Host, reason: "缺少端口"}
	}
	if port != strconv.Itoa(s.port) {
		return &hostError{host: r.Host, reason: "端口不匹配"}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if s.currentConfig().Local.AllowRemote {
		return nil
	}
	return &hostError{host: r.Host, reason: "只允许回环地址"}
}

type hostError struct {
	host   string
	reason string
}

func (e *hostError) Error() string {
	return "Host " + e.host + " 不被允许: " + e.reason
}

func checkJSONContentType(r *http.Request) error {
	raw := r.Header.Get("Content-Type")
	if raw == "" {
		return &headerError{"缺少 Content-Type: application/json"}
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil || mediaType != "application/json" {
		return &headerError{"Content-Type 必须是 application/json"}
	}
	return nil
}

func checkOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return &headerError{"Origin 无法解析"}
	}
	if u.Scheme != "http" || u.Host != r.Host {
		return &headerError{"Origin 与 Host 不一致"}
	}
	return nil
}

type headerError struct{ msg string }

func (e *headerError) Error() string { return e.msg }

func (s *Server) authenticate(r *http.Request) bool {
	token := bearerToken(r)
	if token == "" {
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
	}
	return token != "" && TokenEqual(token, s.token)
}

func bearerToken(r *http.Request) string {
	raw := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(raw) > len(prefix) && strings.EqualFold(raw[:len(prefix)], prefix) {
		return strings.TrimSpace(raw[len(prefix):])
	}
	return ""
}

// serveStatic 托管 Web GUI 静态资源并处理 SPA fallback.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, rid string) {
	if !isSafeMethod(r.Method) {
		methodNotAllowed(w, r, rid)
		return
	}
	p := path.Clean(r.URL.Path)
	if p == "/" || p == "." {
		s.writeIndex(w, r, rid)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		s.writeIndex(w, r, rid)
		return
	}
	data, err := readFile(s.webFS, name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") {
			writeProblem(w, rid, http.StatusNotFound, protocol.CodeNotFound, "资源不存在", "静态资源 "+name+" 不存在")
			return
		}
		s.writeIndex(w, r, rid)
		return
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", ctype)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

func (s *Server) writeIndex(w http.ResponseWriter, r *http.Request, rid string) {
	data, err := readFile(s.webFS, "index.html")
	if err != nil {
		writeProblem(w, rid, http.StatusInternalServerError, protocol.CodeInternal, "Web 资源缺失", "index.html 未打包进二进制")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

// readFile 读取静态资源.
func readFile(fsys fs.FS, name string) ([]byte, error) {
	return fs.ReadFile(fsys, name)
}
