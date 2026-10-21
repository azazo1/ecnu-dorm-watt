package localapi

import (
	"net/http"
	"testing"
)

func TestProxyPathMapping(t *testing.T) {
	cases := []struct {
		method   string
		path     string
		wantPath string
		exists   bool
		allowed  bool
	}{
		{http.MethodGet, "/api/degree", "/v1/degree", true, true},
		{http.MethodPost, "/api/degree", "/v1/degree", true, false},
		{http.MethodGet, "/api/records", "/v1/records", true, true},
		{http.MethodGet, "/api/rooms", "/v1/rooms", true, true},
		{http.MethodPost, "/api/rooms", "/v1/rooms", true, true},
		{http.MethodGet, "/api/rooms/active", "/v1/rooms/active", true, true},
		{http.MethodDelete, "/api/rooms/active", "/v1/rooms/active", true, true},
		{http.MethodPut, "/api/rooms/1/active", "/v1/rooms/1/active", true, true},
		{http.MethodGet, "/api/rooms/1/info", "/v1/rooms/1/info", true, true},
		{http.MethodDelete, "/api/rooms/1/info", "/v1/rooms/1/info", true, false},
		{http.MethodGet, "/api/ecnu/districts", "/v1/ecnu/districts", true, true},
		{http.MethodPut, "/api/session", "/v1/session", true, true},
		{http.MethodGet, "/api/archives/202601", "/v1/archives/202601", true, true},
		{http.MethodDelete, "/api/archives/202601", "/v1/archives/202601", true, true},
		{http.MethodGet, "/api/unknown", "", false, false},
		{http.MethodGet, "/api/archives", "/v1/archives", true, true},
		{http.MethodGet, "/api/rooms/abc/info", "", false, false},
		{http.MethodGet, "/api/archives/202601/extra", "", false, false},
	}
	for _, tc := range cases {
		req, err := http.NewRequest(tc.method, "http://127.0.0.1:1"+tc.path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		got, exists, allowed := proxyPath(req)
		if exists != tc.exists || allowed != tc.allowed || got != tc.wantPath {
			t.Fatalf("%s %s -> (%q, %v, %v), want (%q, %v, %v)",
				tc.method, tc.path, got, exists, allowed, tc.wantPath, tc.exists, tc.allowed)
		}
	}
}
