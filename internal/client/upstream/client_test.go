package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDegreeKeepsBasePathAndQuery(t *testing.T) {
	var (
		gotPath  string
		gotQuery string
		gotAuth  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"degree":33.43,"queried_at":"2026-01-24T14:35:32+08:00","source":"upstream"}`)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/dormwatt/api/", Token: "dw_test_token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	degree, err := c.Degree(context.Background(), true)
	if err != nil {
		t.Fatalf("Degree: %v", err)
	}
	if degree.Degree != 33.43 {
		t.Fatalf("degree = %v", degree.Degree)
	}
	if gotPath != "/dormwatt/api/v1/degree" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "fresh=true" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotAuth != "Bearer dw_test_token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
}

func TestTimeoutFromConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("expected timeout error")
	} else if !IsTransport(err) {
		t.Fatalf("expected transport error, got %T %v", err, err)
	}
}

func TestProblemMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"type":"about:blank","title":"no room","status":409,"code":"room_not_configured","detail":"room is not configured"}`)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.ActiveRoom(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsCode(err, "room_not_configured") {
		t.Fatalf("expected room_not_configured, got %v", err)
	}
	ae, ok := IsAPIError(err)
	if !ok || ae.Status != http.StatusConflict {
		t.Fatalf("unexpected APIError: %#v", ae)
	}
}

func TestArchiveNameIsEscapedOnce(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/csv")
		io.WriteString(w, "time,degree\n")
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/dormwatt/api"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.DownloadArchive(context.Background(), "2026 一月", "", io.Discard); err != nil {
		t.Fatalf("DownloadArchive: %v", err)
	}
	if gotPath != "/dormwatt/api/v1/archives/2026 一月" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestRecordsQueryTimeFormat(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		io.WriteString(w, `{"room_id":1,"room_no":"x","records":[]}`)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/api"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	to := from.Add(24 * time.Hour)
	if _, err := c.Records(context.Background(), RecordsOptions{From: &from, To: &to, Limit: 10}); err != nil {
		t.Fatalf("Records: %v", err)
	}
	for _, want := range []string{"from=2026-01-01T00%3A00%3A00%2B08%3A00", "limit=10"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q does not contain %q", gotQuery, want)
		}
	}
}
