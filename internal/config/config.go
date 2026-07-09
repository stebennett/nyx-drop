// Package config parses and validates the process environment into a
// typed Config, failing fast (naming the offending variable) on any
// missing-or-invalid value. See spec "Configuration (environment
// variables)".
package config

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config holds the fully parsed and validated process configuration.
type Config struct {
	BaseDomain         string // lowercased, validated DNS name
	Scheme             string // "http" | "https"
	TTL                time.Duration
	UploadToken        string
	GitHubClientID     string
	GitHubClientSecret string
	AdminGitHubUser    string // lowercased
	SessionSecret      string
	DataDir            string
	MaxUploadSize      int64
	MaxSiteSize        int64
	MaxFileCount       int
	Port               int
	LogLevel           slog.Level
}

// baseDomainRE matches a multi-label DNS hostname: lowercase letters,
// digits, and hyphens per label (not leading/trailing hyphen), at least
// two labels joined by dots.
var baseDomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Load reads and validates all configuration from getenv, applying
// defaults for optional variables. main passes os.Getenv; tests pass a
// map-backed function so no process env mutation is required.
func Load(getenv func(string) string) (*Config, error) {
	cfg := &Config{}

	baseDomain := strings.ToLower(strings.TrimSpace(getenv("BASE_DOMAIN")))
	if baseDomain == "" {
		return nil, fmt.Errorf("BASE_DOMAIN is required")
	}
	if strings.ContainsAny(baseDomain, ":/") || strings.ContainsAny(baseDomain, " \t\n") || !baseDomainRE.MatchString(baseDomain) {
		return nil, fmt.Errorf("BASE_DOMAIN %q is invalid: must be a multi-label hostname with no scheme, port, or path", getenv("BASE_DOMAIN"))
	}
	cfg.BaseDomain = baseDomain

	scheme := stringOr(getenv("SCHEME"), "https")
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("SCHEME %q is invalid: must be http or https", scheme)
	}
	cfg.Scheme = scheme

	ttlStr := stringOr(getenv("TTL"), "24h")
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		return nil, fmt.Errorf("TTL %q is invalid: %w", ttlStr, err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("TTL %q is invalid: must be greater than zero", ttlStr)
	}
	cfg.TTL = ttl

	uploadToken := getenv("UPLOAD_TOKEN")
	if uploadToken == "" {
		return nil, fmt.Errorf("UPLOAD_TOKEN is required")
	}
	cfg.UploadToken = uploadToken

	clientID := getenv("GITHUB_CLIENT_ID")
	if clientID == "" {
		return nil, fmt.Errorf("GITHUB_CLIENT_ID is required")
	}
	cfg.GitHubClientID = clientID

	clientSecret := getenv("GITHUB_CLIENT_SECRET")
	if clientSecret == "" {
		return nil, fmt.Errorf("GITHUB_CLIENT_SECRET is required")
	}
	cfg.GitHubClientSecret = clientSecret

	adminUser := strings.ToLower(strings.TrimSpace(getenv("ADMIN_GITHUB_USER")))
	if adminUser == "" {
		return nil, fmt.Errorf("ADMIN_GITHUB_USER is required")
	}
	cfg.AdminGitHubUser = adminUser

	sessionSecret := getenv("SESSION_SECRET")
	if sessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET is required")
	}
	cfg.SessionSecret = sessionSecret

	dataDir := stringOr(getenv("DATA_DIR"), "/data")
	if dataDir == "" {
		return nil, fmt.Errorf("DATA_DIR must not be empty")
	}
	cfg.DataDir = dataDir

	maxUploadSizeStr := stringOr(getenv("MAX_UPLOAD_SIZE"), "100MB")
	maxUploadSize, err := ParseSize(maxUploadSizeStr)
	if err != nil {
		return nil, fmt.Errorf("MAX_UPLOAD_SIZE %q is invalid: %w", maxUploadSizeStr, err)
	}
	if maxUploadSize <= 0 {
		return nil, fmt.Errorf("MAX_UPLOAD_SIZE %q is invalid: must be greater than zero", maxUploadSizeStr)
	}
	cfg.MaxUploadSize = maxUploadSize

	maxSiteSizeStr := stringOr(getenv("MAX_SITE_SIZE"), "500MB")
	maxSiteSize, err := ParseSize(maxSiteSizeStr)
	if err != nil {
		return nil, fmt.Errorf("MAX_SITE_SIZE %q is invalid: %w", maxSiteSizeStr, err)
	}
	if maxSiteSize <= 0 {
		return nil, fmt.Errorf("MAX_SITE_SIZE %q is invalid: must be greater than zero", maxSiteSizeStr)
	}
	cfg.MaxSiteSize = maxSiteSize

	maxFileCountStr := stringOr(getenv("MAX_FILE_COUNT"), "10000")
	maxFileCount, err := strconv.Atoi(maxFileCountStr)
	if err != nil {
		return nil, fmt.Errorf("MAX_FILE_COUNT %q is invalid: %w", maxFileCountStr, err)
	}
	if maxFileCount <= 0 {
		return nil, fmt.Errorf("MAX_FILE_COUNT %q is invalid: must be greater than zero", maxFileCountStr)
	}
	cfg.MaxFileCount = maxFileCount

	portStr := stringOr(getenv("PORT"), "8080")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("PORT %q is invalid: %w", portStr, err)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("PORT %q is invalid: must be between 1 and 65535", portStr)
	}
	cfg.Port = port

	logLevelStr := strings.ToLower(stringOr(getenv("LOG_LEVEL"), "info"))
	logLevel, err := parseLogLevel(logLevelStr)
	if err != nil {
		return nil, fmt.Errorf("LOG_LEVEL %q is invalid: %w", logLevelStr, err)
	}
	cfg.LogLevel = logLevel

	return cfg, nil
}

// Addr returns the listen address for http.Server ("":Port").
func (c *Config) Addr() string {
	return ":" + strconv.Itoa(c.Port)
}

// ExternalOrigin returns the scheme+host used to build public URLs and
// validate the Origin header (e.g. "https://sites.nyxhub.net").
func (c *Config) ExternalOrigin() string {
	return c.Scheme + "://" + c.BaseDomain
}

func stringOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseLogLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("must be one of debug, info, warn, error")
	}
}
