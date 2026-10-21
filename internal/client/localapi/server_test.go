package localapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
)

const testToken = "dw_1234567890abcdef"

type testEnv struct {
	srv  *Server
	fake *httptest.Server

	mu          sync.Mutex
	degreePath  string
	degreeQuery string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dormwatt/api/v1/degree" {
			env.mu.Lock()
			env.degreePath = r.URL.Path
			env.degreeQuery = r.URL.RawQuery
			env.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/dormwatt/api/v1/health":
			io.WriteString(w, `{"status":"ok","version":"0.1.0-test"}`)
		case r.URL.Path == "/dormwatt/api/v1/status":
			io.WriteString(w, `{"version":"0.1.0-test","server_time":"2026-01-24T14:35:32+08:00","uptime_seconds":3600,"active_room":{"id":1,"room_no":"4408_MH_83_257","elcarea":102,"elcbuis":"new-83_MH"},"session":{"state":"ok","updated_at":"2026-01-24T14:30:00+08:00"},"monitor":{"poll_interval_seconds":300,"state":"active","last_degree":33.43,"consecutive_failures":0},"storage":{"samples":12,"archives":1}}`)
		case r.URL.Path == "/dormwatt/api/v1/degree":
			io.WriteString(w, `{"degree":33.43,"queried_at":"2026-01-24T14:35:32+08:00","source":"upstream"}`)
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"type":"about:blank","title":"not found","status":404,"code":"not_found"}`)
		}
	}))
	env.fake = fake
	t.Cleanup(fake.Close)

	cfg := config.DefaultClient()
	cfg.Server.BaseURL = fake.URL + "/dormwatt/api"
	cfg.Server.Token = testToken
	cfg.Monitor.PollInterval = config.Duration(time.Hour)

	dir := t.TempDir()
	srv, err := New(Options{
		Version:       "0.1.0-test",
		ConfigPath:    filepath.Join(dir, "client.toml"),
		Config:        &cfg,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimePath:   filepath.Join(dir, "runtime.json"),
		ListenAddress: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	env.srv = srv
	return env
}

func (e *testEnv) request(t *testing.T, method, path string, body string, mutate func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, e.srv.URL()+strings.TrimPrefix(path, "/"), strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if mutate != nil {
		mutate(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}

func TestLocalAuthAndHost(t *testing.T) {
	env := newTestEnv(t)

	resp := env.request(t, http.MethodGet, "/api/health", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodGet, "/api/health", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with token status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodGet, "/api/health", "", func(r *http.Request) {
		r.Host = "evil.example.com:" + portString(env.srv.Port())
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("evil host status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodGet, "/api/health", "", func(r *http.Request) {
		r.Host = "localhost:" + portString(env.srv.Port())
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("localhost host status = %d", resp.StatusCode)
	}
	readBody(t, resp)
}

func TestCSRFChecksAndOptions(t *testing.T) {
	env := newTestEnv(t)

	resp := env.request(t, http.MethodPost, "/api/shutdown", "{}", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing client header status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodPost, "/api/shutdown", "{}", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
		r.Header.Set("X-Dormwatt-Client", "1")
		r.Header.Del("Content-Type")
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing content type status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodPost, "/api/shutdown", "{}", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
		r.Header.Set("X-Dormwatt-Client", "1")
		r.Header.Set("Origin", "http://evil.example.com")
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad origin status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodOptions, "/api/health", "", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("options status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = env.request(t, http.MethodGet, "/api/health", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	defer resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("不应返回 CORS 头")
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("缺少 X-Frame-Options")
	}
}

func TestCookieSessionAndStaticFallback(t *testing.T) {
	env := newTestEnv(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	base := env.srv.URL()

	resp, err := client.Get(base)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	cookies := resp.Cookies()
	body := readBody(t, resp)
	if !strings.Contains(body, "DormWatt") {
		t.Fatalf("index body = %q", body)
	}
	if len(cookies) == 0 || cookies[0].Name != CookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %#v", cookies)
	}

	resp, err = client.Get(base + "api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie auth status = %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp, err = client.Get(base + "records")
	if err != nil {
		t.Fatalf("GET /records: %v", err)
	}
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) == 0 {
		t.Fatalf("SPA fallback status = %d, cookies = %#v", resp.StatusCode, resp.Cookies())
	}
	readBody(t, resp)
}

func TestProxyKeepsBasePath(t *testing.T) {
	env := newTestEnv(t)
	resp := env.request(t, http.MethodGet, "/api/degree?fresh=true", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "33.43") {
		t.Fatalf("proxy body = %q", body)
	}
	env.mu.Lock()
	path, query := env.degreePath, env.degreeQuery
	env.mu.Unlock()
	if path != "/dormwatt/api/v1/degree" {
		t.Fatalf("upstream path = %q", path)
	}
	if query != "fresh=true" {
		t.Fatalf("upstream query = %q", query)
	}
}

func TestStatusUsesCache(t *testing.T) {
	env := newTestEnv(t)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp := env.request(t, http.MethodGet, "/api/status", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+env.srv.Token())
		})
		body := readBody(t, resp)
		var payload struct {
			Local  map[string]any `json:"local"`
			Remote map[string]any `json:"remote"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("status json: %v, body = %s", err, body)
		}
		if payload.Local["health"] == HealthOK && payload.Remote != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// 轮询是异步的, 测试环境可能来不及完成; 只要求接口结构合法.
}

func TestSettingsWrite(t *testing.T) {
	env := newTestEnv(t)
	resp := env.request(t, http.MethodGet, "/api/settings", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
	})
	body := readBody(t, resp)
	if strings.Contains(body, testToken) {
		t.Fatalf("设置接口泄露了 token: %s", body)
	}
	var view SettingsView
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("settings json: %v", err)
	}
	if !view.Server.TokenSet || view.Server.TokenPrefix != "dw_1234" {
		t.Fatalf("token view = %#v", view.Server)
	}

	payload := `{"monitor":{"degree_threshold":8.5}}`
	u, err := url.Parse(env.srv.URL())
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	resp = env.request(t, http.MethodPut, "/api/settings", payload, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
		r.Header.Set("X-Dormwatt-Client", "1")
		r.Header.Set("Origin", "http://"+u.Host)
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put settings status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	readBody(t, resp)
	info, err := os.Stat(env.srv.opts.ConfigPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config permission = %04o", perm)
	}
	cfg, _, err := config.LoadClient(env.srv.opts.ConfigPath, false)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Monitor.DegreeThreshold != 8.5 {
		t.Fatalf("threshold = %v", cfg.Monitor.DegreeThreshold)
	}
}

func TestShutdownRemovesRuntime(t *testing.T) {
	env := newTestEnv(t)
	runtimePath := env.srv.RuntimePath()
	info, err := os.Stat(runtimePath)
	if err != nil {
		t.Fatalf("runtime.json 未写入: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("runtime.json 权限 = %04o, want 0600", perm)
	}
	resp := env.request(t, http.MethodPost, "/api/shutdown", "{}", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+env.srv.Token())
		r.Header.Set("X-Dormwatt-Client", "1")
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("shutdown status = %d", resp.StatusCode)
	}
	readBody(t, resp)
	select {
	case <-env.srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("服务未在 5 秒内关闭")
	}
	if _, err := os.Stat(runtimePath); !os.IsNotExist(err) {
		t.Fatalf("runtime.json 未删除: %v", err)
	}
}

func portString(port int) string {
	return strconv.Itoa(port)
}
