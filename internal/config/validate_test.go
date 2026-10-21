package config

import "testing"

func TestDefaultConfigsValidate(t *testing.T) {
	server := DefaultServer()
	if err := ValidateServer(&server); err != nil {
		t.Fatalf("default server invalid: %v", err)
	}
	client := DefaultClient()
	if err := ValidateClient(&client); err != nil {
		t.Fatalf("default client invalid: %v", err)
	}
}

func TestClientBaseURL(t *testing.T) {
	cfg := DefaultClient()
	cfg.Server.BaseURL = "https://example.com/dormwatt/api/"
	if err := ValidateClient(&cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.Server.BaseURL != "https://example.com/dormwatt/api" {
		t.Fatalf("trailing slash not trimmed: %q", cfg.Server.BaseURL)
	}
	cfg.Server.BaseURL = "ftp://example.com"
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("ftp URL accepted")
	}
}
