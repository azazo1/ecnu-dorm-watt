package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

type recordedSample struct {
	roomID int64
	at     time.Time
	degree float64
}

// fakeDB 只实现采集循环用到的方法, 其余方法由嵌入的 nil 接口兜底.
type fakeDB struct {
	store.DB

	mu           sync.Mutex
	room         *store.Room
	session      *store.Session
	lastSample   *store.Sample
	samples      []recordedSample
	sessionState string
}

func (f *fakeDB) ActiveRoom(context.Context) (*store.Room, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.room == nil {
		return nil, store.ErrNotFound
	}
	room := *f.room
	return &room, nil
}

func (f *fakeDB) GetSession(context.Context) (*store.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.session == nil {
		return nil, store.ErrNotFound
	}
	session := *f.session
	return &session, nil
}

func (f *fakeDB) LastSample(context.Context, int64) (*store.Sample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastSample == nil {
		return nil, store.ErrNotFound
	}
	sample := *f.lastSample
	return &sample, nil
}

func (f *fakeDB) InsertSample(_ context.Context, roomID int64, at time.Time, degree float64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.samples = append(f.samples, recordedSample{roomID: roomID, at: at, degree: degree})
	f.lastSample = &store.Sample{Time: at, Degree: degree}
	return true, nil
}

func (f *fakeDB) SetSessionState(_ context.Context, state string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionState = state
	return nil
}

func (f *fakeDB) sampleCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.samples)
}

func (f *fakeDB) lastRecorded() (recordedSample, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.samples) == 0 {
		return recordedSample{}, false
	}
	return f.samples[len(f.samples)-1], true
}

// fakeUpstream 是可调整响应的上游服务.
type fakeUpstream struct {
	mu          sync.Mutex
	statusCode  int
	contentType string
	body        string
	hits        int
}

func (u *fakeUpstream) setResponse(statusCode int, contentType, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.statusCode = statusCode
	u.contentType = contentType
	u.body = body
}

func (u *fakeUpstream) setDegree(degree string) {
	u.setResponse(http.StatusOK, "application/json", `{"retcode":0,"retmsg":"成功","restElecDegree":`+degree+`}`)
}

func (u *fakeUpstream) hitsCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits
}

func (u *fakeUpstream) serve(w http.ResponseWriter, _ *http.Request) {
	u.mu.Lock()
	statusCode := u.statusCode
	contentType := u.contentType
	body := u.body
	u.hits++
	u.mu.Unlock()

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	_, _ = w.Write([]byte(body))
}

func newMonitorForTest(t *testing.T, db store.DB, upstream *fakeUpstream) *Monitor {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(upstream.serve))
	t.Cleanup(srv.Close)

	client, err := ecnu.New(ecnu.Options{
		BaseURL:   srv.URL,
		Timeout:   3 * time.Second,
		UserAgent: "monitor-test",
	})
	if err != nil {
		t.Fatalf("构造 ECNU 客户端失败: %v", err)
	}
	m, err := New(Options{
		DB:     db,
		Client: client,
		Config: config.ECNUSection{
			PollInterval:          config.Duration(300 * time.Second),
			DegreeChangeThreshold: 0.01,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("构造 Monitor 失败: %v", err)
	}
	return m
}

func newUpstream() *fakeUpstream {
	upstream := &fakeUpstream{}
	upstream.setDegree("10")
	return upstream
}

func activeDB() *fakeDB {
	return &fakeDB{
		room: &store.Room{
			ID:      1,
			RoomNo:  "4408_MH_83_257",
			Elcarea: 102,
			Elcbuis: "new-83_MH",
		},
		session: &store.Session{
			JSESSIONID: "jsession",
			Cookie:     "cookie",
			CSRFToken:  "csrf",
			State:      "ok",
		},
	}
}

func TestPollIdleWithoutRoom(t *testing.T) {
	upstream := newUpstream()
	db := &fakeDB{}
	m := newMonitorForTest(t, db, upstream)

	if delay := m.poll(context.Background()); delay != 300*time.Second {
		t.Fatalf("空房间时延迟错误: %v", delay)
	}
	status := m.Status()
	if status.State != StateIdle {
		t.Fatalf("状态错误: %s", status.State)
	}
	if status.NextPollAt == nil || status.ConsecutiveFailures != 0 {
		t.Fatalf("状态字段错误: %+v", status)
	}
	if upstream.hitsCount() != 0 {
		t.Fatalf("空房间时不应请求上游, 实际 %d 次", upstream.hitsCount())
	}
}

func TestPollNoSession(t *testing.T) {
	upstream := newUpstream()
	db := activeDB()
	db.session = nil
	m := newMonitorForTest(t, db, upstream)

	delay := m.poll(context.Background())
	if delay != SessionProbeInterval {
		t.Fatalf("无会话时延迟错误: %v", delay)
	}
	if status := m.Status(); status.State != StateNoSession {
		t.Fatalf("状态错误: %s", status.State)
	}
	if upstream.hitsCount() != 0 {
		t.Fatalf("无会话时不应请求上游, 实际 %d 次", upstream.hitsCount())
	}
}

func TestPollInvalidSessionSkipsUpstream(t *testing.T) {
	upstream := newUpstream()
	db := activeDB()
	db.session.State = "invalid"
	m := newMonitorForTest(t, db, upstream)

	if delay := m.poll(context.Background()); delay != SessionProbeInterval {
		t.Fatalf("会话失效时延迟错误: %v", delay)
	}
	if status := m.Status(); status.State != StateNoSession {
		t.Fatalf("状态错误: %s", status.State)
	}
	if upstream.hitsCount() != 0 {
		t.Fatalf("会话失效时不应请求上游, 实际 %d 次", upstream.hitsCount())
	}
}

func TestPollRecordsSamples(t *testing.T) {
	upstream := newUpstream()
	db := activeDB()
	db.session.State = "unknown"
	m := newMonitorForTest(t, db, upstream)
	ctx := context.Background()

	delay := m.poll(ctx)
	if delay < 270*time.Second || delay > 330*time.Second {
		t.Fatalf("成功时延迟未按抖动计算: %v", delay)
	}
	status := m.Status()
	if status.State != StateActive {
		t.Fatalf("状态错误: %s", status.State)
	}
	if status.LastDegree == nil || *status.LastDegree != 10 {
		t.Fatalf("last_degree 错误: %+v", status.LastDegree)
	}
	if status.LastSuccessAt == nil || status.LastAttemptAt == nil || status.LastError != nil {
		t.Fatalf("状态字段错误: %+v", status)
	}
	if status.ConsecutiveFailures != 0 {
		t.Fatalf("失败计数应为 0: %d", status.ConsecutiveFailures)
	}
	if db.sampleCount() != 1 {
		t.Fatalf("应记录一条样本: %d", db.sampleCount())
	}
	if sample, ok := db.lastRecorded(); !ok || sample.degree != 10 || sample.at.Nanosecond() != 0 || sample.roomID != 1 {
		t.Fatalf("样本内容错误: %+v", sample)
	}
	if db.sessionState != "ok" {
		t.Fatalf("成功后应回写会话状态: %q", db.sessionState)
	}

	// 变化小于阈值时不记录.
	upstream.setDegree("10.004")
	m.poll(ctx)
	if db.sampleCount() != 1 {
		t.Fatalf("阈值内不应记录新样本: %d", db.sampleCount())
	}

	// 变化达到阈值时记录, 并四舍五入到两位小数.
	upstream.setDegree("10.506")
	m.poll(ctx)
	if db.sampleCount() != 2 {
		t.Fatalf("应记录第二条样本: %d", db.sampleCount())
	}
	if sample, _ := db.lastRecorded(); sample.degree != 10.51 {
		t.Fatalf("样本度数未取整: %+v", sample)
	}
}

func TestPollDegradedBackoff(t *testing.T) {
	upstream := newUpstream()
	upstream.setResponse(http.StatusInternalServerError, "application/json", `{}`)
	db := activeDB()
	m := newMonitorForTest(t, db, upstream)
	ctx := context.Background()

	if delay := m.poll(ctx); delay != 300*time.Second {
		t.Fatalf("首次失败延迟错误: %v", delay)
	}
	status := m.Status()
	if status.State != StateDegraded || status.ConsecutiveFailures != 1 {
		t.Fatalf("失败后状态错误: %+v", status)
	}
	if status.LastError == nil || status.LastError.Code != protocol.CodeECNUUpstream {
		t.Fatalf("last_error 错误: %+v", status.LastError)
	}
	if status.NextPollAt == nil {
		t.Fatal("next_poll_at 应存在")
	}

	if delay := m.poll(ctx); delay != 600*time.Second {
		t.Fatalf("第二次失败延迟错误: %v", delay)
	}
	if failures := m.Status().ConsecutiveFailures; failures != 2 {
		t.Fatalf("失败计数错误: %d", failures)
	}

	upstream.setDegree("12")
	if delay := m.poll(ctx); delay < 270*time.Second || delay > 330*time.Second {
		t.Fatalf("恢复后延迟错误: %v", delay)
	}
	status = m.Status()
	if status.State != StateActive || status.ConsecutiveFailures != 0 || status.LastError != nil {
		t.Fatalf("恢复后状态错误: %+v", status)
	}
}

func TestPollSessionExpired(t *testing.T) {
	upstream := newUpstream()
	upstream.setResponse(http.StatusOK, "text/html", "<html>login</html>")
	db := activeDB()
	m := newMonitorForTest(t, db, upstream)

	delay := m.poll(context.Background())
	if delay != SessionProbeInterval {
		t.Fatalf("会话失效延迟错误: %v", delay)
	}
	status := m.Status()
	if status.State != StateNoSession {
		t.Fatalf("状态错误: %s", status.State)
	}
	if status.LastError == nil || status.LastError.Code != protocol.CodeECNUNotLoggedIn {
		t.Fatalf("last_error 错误: %+v", status.LastError)
	}
	if db.sessionState != "invalid" {
		t.Fatalf("应标记会话失效: %q", db.sessionState)
	}
}

func TestBackoff(t *testing.T) {
	m := &Monitor{cfg: config.ECNUSection{PollInterval: config.Duration(10 * time.Second)}}
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: 10 * time.Second},
		{failures: 1, want: 10 * time.Second},
		{failures: 2, want: 20 * time.Second},
		{failures: 3, want: 40 * time.Second},
		{failures: 100, want: MaxBackoff},
	}
	for _, tc := range cases {
		if got := m.backoff(tc.failures); got != tc.want {
			t.Errorf("backoff(%d) = %v, 期望 %v", tc.failures, got, tc.want)
		}
	}
}

func TestStartStopsOnContextCancel(t *testing.T) {
	upstream := newUpstream()
	db := activeDB()
	m := newMonitorForTest(t, db, upstream)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Start(ctx)
		close(done)
	}()

	// 初始延迟最多 2 秒, Wake 应立即触发一次查询.
	m.Wake()
	deadline := time.Now().Add(5 * time.Second)
	for m.Status().State != StateActive {
		if time.Now().After(deadline) {
			t.Fatalf("未在超时前开始采集: %+v", m.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后循环未退出")
	}
	if m.StartedAt().IsZero() {
		t.Fatal("Start 应记录开始时间")
	}
}

func TestWakeDoesNotBlock(t *testing.T) {
	upstream := newUpstream()
	m := newMonitorForTest(t, &fakeDB{}, upstream)
	for i := 0; i < 10; i++ {
		m.Wake()
	}
}

func TestNewValidatesDependencies(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("缺少数据库时应报错")
	}
	db := &fakeDB{}
	if _, err := New(Options{DB: db}); err == nil {
		t.Fatal("缺少客户端时应报错")
	}
	client, err := ecnu.New(ecnu.Options{})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	if _, err := New(Options{DB: db, Client: client}); err != nil {
		t.Fatalf("构造 Monitor 失败: %v", err)
	}
}

func TestStatusCopiesPointers(t *testing.T) {
	db := activeDB()
	upstream := newUpstream()
	m := newMonitorForTest(t, db, upstream)
	m.poll(context.Background())

	status := m.Status()
	*status.LastDegree = 99
	status.LastError = &protocol.ErrorInfo{Code: "tampered"}
	again := m.Status()
	if again.LastDegree == nil || *again.LastDegree == 99 {
		t.Fatal("Status 不应共享内部指针")
	}
	if again.LastError != nil {
		t.Fatal("Status 不应共享错误指针")
	}
}

func TestLoadInputsErrors(t *testing.T) {
	db := &fakeDB{}
	m := newMonitorForTest(t, db, newUpstream())
	// fakeDB 用 ErrNotFound 表达缺失, 这里确认错误分支不会被当成失败.
	if _, _, err := m.loadInputs(context.Background()); err != nil {
		t.Fatalf("缺失不应返回错误: %v", err)
	}
	if errors.Is(store.ErrNotFound, store.ErrConflict) {
		t.Fatal("哨兵错误不应重叠")
	}
}
