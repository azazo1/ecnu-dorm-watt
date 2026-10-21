package protocol

import (
	"testing"
	"time"
)

func TestJoinBaseURL(t *testing.T) {
	tests := []struct {
		base, suffix, want string
	}{
		{"https://example.com/dormwatt/api", "/v1/records", "https://example.com/dormwatt/api/v1/records"},
		{"https://example.com/dormwatt/api/", "v1/records", "https://example.com/dormwatt/api/v1/records"},
		{"http://127.0.0.1:20531", "/v1/health", "http://127.0.0.1:20531/v1/health"},
	}
	for _, tt := range tests {
		got, err := JoinBaseURL(tt.base, tt.suffix)
		if err != nil {
			t.Fatalf("JoinBaseURL: %v", err)
		}
		if got != tt.want {
			t.Fatalf("got %q want %q", got, tt.want)
		}
	}
}

func TestParseTimeRequiresOffset(t *testing.T) {
	if _, err := ParseTime("2026-01-24T14:35:32+08:00"); err != nil {
		t.Fatalf("valid time rejected: %v", err)
	}
	if _, err := ParseTime("2026-01-24T14:35:32"); err == nil {
		t.Fatal("time without offset accepted")
	}
}

func TestFormatTime(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	got := FormatTime(time.Date(2026, 1, 24, 6, 35, 32, 0, time.UTC), loc)
	want := "2026-01-24T14:35:32+08:00"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
