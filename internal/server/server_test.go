package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// testBasePath 是默认测试挂载前缀.
const testBasePath = "/dormwatt/api"

// newTestServer 构造测试用 Server, 默认前缀为 /dormwatt/api.
func newTestServer(t *testing.T, mutate func(*Deps)) (*Server, *fakeStore) {
	t.Helper()
	db := newFakeStore()
	cfg := config.DefaultServer()
	cfg.Server.BasePath = testBasePath
	deps := Deps{
		DB:        db,
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
		StartedAt: time.Now().Add(-2 * time.Minute),
	}
	if mutate != nil {
		mutate(&deps)
	}
	return New(deps), db
}

// doRequest 发起一次测试请求.
func doRequest(t *testing.T, srv *Server, method, path, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// problemOf 解析 problem 响应.
func problemOf(t *testing.T, rec *httptest.ResponseRecorder) protocol.Problem {
	t.Helper()
	var p protocol.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("解析 problem 失败: %v, body=%s", err, rec.Body.String())
	}
	return p
}

// sha256Hex 计算 token 摘要.
func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestHealthAndRequestID(t *testing.T) {
	srv, _ := newTestServer(t, nil)
	rec := doRequest(t, srv, http.MethodGet, testBasePath+"/v1/health", "", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health 状态码 = %d, 期望 200", rec.Code)
	}
	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 health 响应失败: %v", err)
	}
	if body.Status != "ok" || body.Version != "test" {
		t.Fatalf("health 响应 = %+v", body)
	}
	generated := rec.Header().Get("X-Request-Id")
	if !requestIDPattern.MatchString(generated) {
		t.Fatalf("生成的请求 id 不合法: %q", generated)
	}

	rec = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/health", "", "", map[string]string{"X-Request-Id": "client-request-01"})
	if got := rec.Header().Get("X-Request-Id"); got != "client-request-01" {
		t.Fatalf("应回传客户端请求 id, 实际 %q", got)
	}

	rec = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/health", "", "", map[string]string{"X-Request-Id": "bad"})
	if got := rec.Header().Get("X-Request-Id"); got == "bad" || !requestIDPattern.MatchString(got) {
		t.Fatalf("非法请求 id 应被替换, 实际 %q", got)
	}

	srv.MarkShuttingDown()
	rec = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/health", "", "", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("关闭中 health 状态码 = %d, 期望 503", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeUnavailable {
		t.Fatalf("关闭中 health code = %q", code)
	}
}

func TestBasePathMount(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		path   string
		status int
	}{
		{"挂载前缀内", testBasePath, testBasePath + "/v1/health", http.StatusOK},
		{"根挂载", "/", "/v1/health", http.StatusOK},
		{"缺少前缀", testBasePath, "/v1/health", http.StatusNotFound},
		{"只有前缀", testBasePath, testBasePath, http.StatusNotFound},
		{"前缀相邻", testBasePath, testBasePath + "x/v1/health", http.StatusNotFound},
		{"前缀外根路径", testBasePath, "/", http.StatusNotFound},
		{"根挂载的其他路径", "/", "/health", http.StatusNotFound},
		{"末尾斜杠", testBasePath, testBasePath + "/v1/health/", http.StatusNotFound},
		{"未知接口", testBasePath, testBasePath + "/v1/nope", http.StatusNotFound},
		{"旧版本段", testBasePath, testBasePath + "/v2/health", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t, func(deps *Deps) {
				deps.Config.Server.BasePath = tc.base
			})
			rec := doRequest(t, srv, http.MethodGet, tc.path, "", "", nil)
			if rec.Code != tc.status {
				t.Fatalf("路径 %s 状态码 = %d, 期望 %d", tc.path, rec.Code, tc.status)
			}
			if rec.Code == http.StatusNotFound {
				if got := rec.Header().Get("Location"); got != "" {
					t.Fatalf("不应返回重定向, Location=%q", got)
				}
				if code := problemOf(t, rec).Code; code != protocol.CodeNotFound {
					t.Fatalf("404 的 code = %q", code)
				}
			}
		})
	}
}

func TestAuthentication(t *testing.T) {
	srv, db := newTestServer(t, nil)
	readToken := db.addToken("reader", "read")
	writeToken := db.addToken("writer", "write")

	t.Run("缺少 token", func(t *testing.T) {
		rec := doRequest(t, srv, http.MethodGet, testBasePath+"/v1/status", "", "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d, 期望 401", rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="dormwatt"` {
			t.Fatalf("WWW-Authenticate = %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
			t.Fatalf("Content-Type = %q", got)
		}
		if code := problemOf(t, rec).Code; code != protocol.CodeUnauthorized {
			t.Fatalf("code = %q", code)
		}
	})

	t.Run("错误 scheme", func(t *testing.T) {
		rec := doRequest(t, srv, http.MethodGet, testBasePath+"/v1/status", "", "", map[string]string{"Authorization": "Basic abc"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d, 期望 401", rec.Code)
		}
	})

	t.Run("未知 token", func(t *testing.T) {
		rec := doRequest(t, srv, http.MethodGet, testBasePath+"/v1/status", "dw_unknown", "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d, 期望 401", rec.Code)
		}
	})

	t.Run("多个 Authorization 头", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testBasePath+"/v1/status", nil)
		req.Header.Add("Authorization", "Bearer "+readToken)
		req.Header.Add("Authorization", "Bearer "+writeToken)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d, 期望 401", rec.Code)
		}
	})

	t.Run("read token 读取", func(t *testing.T) {
		rec := doRequest(t, srv, http.MethodGet, testBasePath+"/v1/whoami", readToken, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, 期望 200", rec.Code)
		}
		var who protocol.WhoAmI
		if err := json.Unmarshal(rec.Body.Bytes(), &who); err != nil {
			t.Fatalf("解析 whoami 失败: %v", err)
		}
		if who.Name != "reader" || len(who.Scopes) != 1 || who.Scopes[0] != "read" {
			t.Fatalf("whoami = %+v", who)
		}
	})

	t.Run("read token 不能写入", func(t *testing.T) {
		body := `{"jsession_id":"a","cookie":"b","x_csrf_token":"c"}`
		rec := doRequest(t, srv, http.MethodPut, testBasePath+"/v1/session", readToken, body, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态码 = %d, 期望 403", rec.Code)
		}
		if code := problemOf(t, rec).Code; code != protocol.CodeForbidden {
			t.Fatalf("code = %q", code)
		}
	})

	t.Run("静态 token 生效", func(t *testing.T) {
		staticServer, _ := newTestServer(t, func(deps *Deps) {
			deps.StaticTokens = []config.StaticToken{{Name: "static-reader", Scopes: []string{"read"}, SHA256: strings.ToUpper(sha256Hex("dw_static"))}}
		})
		rec := doRequest(t, staticServer, http.MethodGet, testBasePath+"/v1/whoami", "dw_static", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, 期望 200", rec.Code)
		}
		var who protocol.WhoAmI
		if err := json.Unmarshal(rec.Body.Bytes(), &who); err != nil {
			t.Fatalf("解析 whoami 失败: %v", err)
		}
		if who.Name != "static-reader" {
			t.Fatalf("静态 token 名称 = %q", who.Name)
		}
	})
}

func TestSessionNeverReturnsCredentials(t *testing.T) {
	srv, db := newTestServer(t, nil)
	token := db.addToken("writer", "read", "write")

	body := `{"jsession_id":"SECRET-JSESSION","cookie":"SECRET-COOKIE","x_csrf_token":"SECRET-CSRF"}`
	rec := doRequest(t, srv, http.MethodPut, testBasePath+"/v1/session", token, body, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("上传会话状态码 = %d, 期望 204", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/session", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取会话状态码 = %d, 期望 200", rec.Code)
	}
	raw := rec.Body.String()
	for _, secret := range []string{"SECRET", "jsession", "cookie", "csrf"} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(secret)) {
			t.Fatalf("会话响应不应包含 %q: %s", secret, raw)
		}
	}
	var info protocol.SessionInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("解析会话响应失败: %v", err)
	}
	if info.State != "unknown" {
		t.Fatalf("会话状态 = %q", info.State)
	}
	if info.UpdatedAt == nil {
		t.Fatal("会话响应应包含 updated_at")
	}

	t.Run("字段缺失", func(t *testing.T) {
		rec := doRequest(t, srv, http.MethodPut, testBasePath+"/v1/session", token, `{"jsession_id":"a","cookie":""}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("状态码 = %d, 期望 422", rec.Code)
		}
	})

	t.Run("未知字段", func(t *testing.T) {
		body := `{"jsession_id":"a","cookie":"b","x_csrf_token":"c","extra":1}`
		rec := doRequest(t, srv, http.MethodPut, testBasePath+"/v1/session", token, body, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("状态码 = %d, 期望 400", rec.Code)
		}
	})

	t.Run("删除会话", func(t *testing.T) {
		rec := doRequest(t, srv, http.MethodDelete, testBasePath+"/v1/session", token, "", nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("状态码 = %d, 期望 204", rec.Code)
		}
		rec = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/session", token, "", nil)
		var info protocol.SessionInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
			t.Fatalf("解析会话响应失败: %v", err)
		}
		if info.State != "missing" {
			t.Fatalf("会话状态 = %q, 期望 missing", info.State)
		}
	})
}

func TestFailedAuthRateLimit(t *testing.T) {
	srv, _ := newTestServer(t, func(deps *Deps) {
		deps.Config.Auth.FailedAuthPerMinute = 3
	})
	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		last = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/status", "dw_wrong", "", nil)
		if last.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次认证失败状态码 = %d", i+1, last.Code)
		}
	}
	last = doRequest(t, srv, http.MethodGet, testBasePath+"/v1/status", "dw_wrong", "", nil)
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("超过认证失败上限后状态码 = %d, 期望 429", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("429 响应应包含 Retry-After")
	}
	if code := problemOf(t, last).Code; code != protocol.CodeRateLimited {
		t.Fatalf("code = %q", code)
	}
}

// fakeMonitor 是测试用的采集状态.
type fakeMonitor struct {
	status protocol.MonitorStatus
	woken  int
}

func (m *fakeMonitor) Status() protocol.MonitorStatus { return m.status }

func (m *fakeMonitor) Wake() { m.woken++ }

// fakeDegree 是测试用的电量来源.
type fakeDegree struct {
	degree float64
	err    error
	calls  int
}

func (d *fakeDegree) Query(ctx context.Context, room store.Room, session store.Session) (float64, error) {
	d.calls++
	if d.err != nil {
		return 0, d.err
	}
	return d.degree, nil
}
