package config

import (
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"
)

// validEnv is a complete, valid baseline env map; individual tests
// mutate a copy to isolate one variable at a time.
func validEnv() map[string]string {
	return map[string]string{
		"BASE_DOMAIN":          "sites.nyxhub.net",
		"UPLOAD_TOKEN":         "tok",
		"GITHUB_CLIENT_ID":     "id",
		"GITHUB_CLIENT_SECRET": "secret",
		"ADMIN_GITHUB_USER":    "octocat",
		"SESSION_SECRET":       "shh",
	}
}

func getenvFromMap(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoad_AllDefaults(t *testing.T) {
	cfg, err := Load(getenvFromMap(validEnv()))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.BaseDomain != "sites.nyxhub.net" {
		t.Errorf("BaseDomain = %q, want sites.nyxhub.net", cfg.BaseDomain)
	}
	if cfg.Scheme != "https" {
		t.Errorf("Scheme = %q, want https", cfg.Scheme)
	}
	if cfg.TTL != 24*time.Hour {
		t.Errorf("TTL = %v, want 24h", cfg.TTL)
	}
	if cfg.UploadToken != "tok" {
		t.Errorf("UploadToken = %q, want tok", cfg.UploadToken)
	}
	if cfg.GitHubClientID != "id" {
		t.Errorf("GitHubClientID = %q, want id", cfg.GitHubClientID)
	}
	if cfg.GitHubClientSecret != "secret" {
		t.Errorf("GitHubClientSecret = %q, want secret", cfg.GitHubClientSecret)
	}
	if cfg.AdminGitHubUser != "octocat" {
		t.Errorf("AdminGitHubUser = %q, want octocat", cfg.AdminGitHubUser)
	}
	if cfg.SessionSecret != "shh" {
		t.Errorf("SessionSecret = %q, want shh", cfg.SessionSecret)
	}
	if cfg.DataDir != "/data" {
		t.Errorf("DataDir = %q, want /data", cfg.DataDir)
	}
	if cfg.MaxUploadSize != 100_000_000 {
		t.Errorf("MaxUploadSize = %d, want 100000000", cfg.MaxUploadSize)
	}
	if cfg.MaxSiteSize != 500_000_000 {
		t.Errorf("MaxSiteSize = %d, want 500000000", cfg.MaxSiteSize)
	}
	if cfg.MaxFileCount != 10000 {
		t.Errorf("MaxFileCount = %d, want 10000", cfg.MaxFileCount)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestLoad_UppercasesNormalizedFields(t *testing.T) {
	env := validEnv()
	env["BASE_DOMAIN"] = "SITES.NyxHub.NET"
	env["ADMIN_GITHUB_USER"] = "OctoCat"
	cfg, err := Load(getenvFromMap(env))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.BaseDomain != "sites.nyxhub.net" {
		t.Errorf("BaseDomain = %q, want lowercased", cfg.BaseDomain)
	}
	if cfg.AdminGitHubUser != "octocat" {
		t.Errorf("AdminGitHubUser = %q, want lowercased", cfg.AdminGitHubUser)
	}
}

func TestLoad_InvalidAndMissing(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(map[string]string)
		wantVar string
	}{
		{"base_domain missing", func(e map[string]string) { delete(e, "BASE_DOMAIN") }, "BASE_DOMAIN"},
		{"base_domain scheme", func(e map[string]string) { e["BASE_DOMAIN"] = "https://x" }, "BASE_DOMAIN"},
		{"base_domain port", func(e map[string]string) { e["BASE_DOMAIN"] = "x.example:8080" }, "BASE_DOMAIN"},
		{"base_domain path", func(e map[string]string) { e["BASE_DOMAIN"] = "x.example/y" }, "BASE_DOMAIN"},
		{"base_domain single-label", func(e map[string]string) { e["BASE_DOMAIN"] = "localhost" }, "BASE_DOMAIN"},
		{"scheme invalid", func(e map[string]string) { e["SCHEME"] = "ftp" }, "SCHEME"},
		{"ttl unparseable", func(e map[string]string) { e["TTL"] = "banana" }, "TTL"},
		{"ttl zero", func(e map[string]string) { e["TTL"] = "0s" }, "TTL"},
		{"max_upload_size unknown suffix", func(e map[string]string) { e["MAX_UPLOAD_SIZE"] = "10PB" }, "MAX_UPLOAD_SIZE"},
		{"max_file_count negative", func(e map[string]string) { e["MAX_FILE_COUNT"] = "-1" }, "MAX_FILE_COUNT"},
		{"port out of range", func(e map[string]string) { e["PORT"] = "70000" }, "PORT"},
		{"log_level invalid", func(e map[string]string) { e["LOG_LEVEL"] = "trace" }, "LOG_LEVEL"},
		{"upload_token missing", func(e map[string]string) { delete(e, "UPLOAD_TOKEN") }, "UPLOAD_TOKEN"},
		{"github_client_id missing", func(e map[string]string) { delete(e, "GITHUB_CLIENT_ID") }, "GITHUB_CLIENT_ID"},
		{"github_client_secret missing", func(e map[string]string) { delete(e, "GITHUB_CLIENT_SECRET") }, "GITHUB_CLIENT_SECRET"},
		{"admin_github_user missing", func(e map[string]string) { delete(e, "ADMIN_GITHUB_USER") }, "ADMIN_GITHUB_USER"},
		{"session_secret missing", func(e map[string]string) { delete(e, "SESSION_SECRET") }, "SESSION_SECRET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := maps.Clone(validEnv())
			c.mutate(env)
			_, err := Load(getenvFromMap(env))
			if err == nil {
				t.Fatalf("Load() = nil error, want error mentioning %s", c.wantVar)
			}
			if !strings.Contains(err.Error(), c.wantVar) {
				t.Fatalf("Load() error = %q, want it to mention %s", err.Error(), c.wantVar)
			}
		})
	}
}

func TestConfig_Accessors(t *testing.T) {
	cfg, err := Load(getenvFromMap(validEnv()))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got := cfg.Addr(); got != ":8080" {
		t.Errorf("Addr() = %q, want :8080", got)
	}
	if got := cfg.ExternalOrigin(); got != "https://sites.nyxhub.net" {
		t.Errorf("ExternalOrigin() = %q, want https://sites.nyxhub.net", got)
	}
}
