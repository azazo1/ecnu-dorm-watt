package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/server"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

type fakeDegree struct{}

func (fakeDegree) Query(context.Context, store.Room, store.Session) (float64, error) {
	return 33.43, nil
}

type fakeMonitor struct{}

func (fakeMonitor) Status() protocol.MonitorStatus { return protocol.MonitorStatus{State: "active"} }
func (fakeMonitor) Wake()                          {}

type fakeCatalog struct{}

func (fakeCatalog) Districts(context.Context) (protocol.Districts, error) {
	return protocol.Districts{}, nil
}
func (fakeCatalog) Buildings(context.Context, string, string) (protocol.Buildings, error) {
	return protocol.Buildings{}, nil
}
func (fakeCatalog) Floors(context.Context, string, string, string) (protocol.Floors, error) {
	return protocol.Floors{}, nil
}
func (fakeCatalog) Rooms(context.Context, string, string, string, string) (protocol.CatalogRooms, error) {
	return protocol.CatalogRooms{}, nil
}
func (fakeCatalog) RoomInfo(context.Context, store.Room) (server.RoomInfo, error) {
	return server.RoomInfo{}, nil
}

func newTestServer(t *testing.T) (*httptest.Server, store.DB) {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{Path: t.TempDir() + "/test.db"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	token := "dw_testtoken"
	sum := sha256.Sum256([]byte(token))
	if _, err := db.CreateToken(context.Background(), "test", "dw_test", hex.EncodeToString(sum[:]), []string{"read", "write"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultServer()
	api := server.New(server.Deps{
		DB:      db,
		Config:  cfg,
		Version: "test",
		Degree:  fakeDegree{},
		Catalog: fakeCatalog{},
		Monitor: fakeMonitor{},
	})
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return ts, db
}

func doRequest(t *testing.T, ts *httptest.Server, method, path string, body any, token string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func TestServerAuth(t *testing.T) {
	ts, db := newTestServer(t)

	resp, _ := doRequest(t, ts, http.MethodGet, "/v1/health", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	resp, _ = doRequest(t, ts, http.MethodGet, "/dormwatt/api/v1/health", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("old prefix status = %d, want 404", resp.StatusCode)
	}
	resp, _ = doRequest(t, ts, http.MethodGet, "/v1/status", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", resp.StatusCode)
	}
	resp, body := doRequest(t, ts, http.MethodGet, "/v1/status", nil, "dw_testtoken")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var status protocol.Status
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}

	room, _, err := db.EnsureRoom(context.Background(), "4408_MH_83_257", 102, "new-83_MH", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetActiveRoom(context.Background(), room.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertSample(context.Background(), room.ID, time.Date(2026, 1, 24, 14, 35, 32, 0, time.FixedZone("CST", 8*3600)), 33.43); err != nil {
		t.Fatal(err)
	}
	resp, body = doRequest(t, ts, http.MethodGet, "/v1/records", nil, "dw_testtoken")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("records = %d body=%s", resp.StatusCode, body)
	}
	var records protocol.Records
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 1 {
		t.Fatalf("records len = %d", len(records.Records))
	}

	archiveBody := map[string]any{"name": "test-archive"}
	resp, body = doRequest(t, ts, http.MethodPost, "/v1/archives", archiveBody, "dw_testtoken")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create archive = %d body=%s", resp.StatusCode, body)
	}
	resp, body = doRequest(t, ts, http.MethodGet, "/v1/archives", nil, "dw_testtoken")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "test-archive") {
		t.Fatalf("list archives = %d body=%s", resp.StatusCode, body)
	}
	resp, body = doRequest(t, ts, http.MethodGet, "/v1/archives/test-archive", nil, "dw_testtoken")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "33.43") {
		t.Fatalf("download archive = %d body=%s", resp.StatusCode, body)
	}
}
