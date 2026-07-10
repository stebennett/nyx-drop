package server

import (
	"net/http"
	"strings"

	"nyx-drop/internal/config"
)

// normalizeHost lowercases the Host header and strips any :port, per spec
// "Static serving" host normalization. Empty input returns empty output.
// Only a syntactically valid numeric :port suffix is stripped (bare
// "host:port" or bracketed "[ipv6]:port"/"[ipv6]"); anything else is left
// untouched, so the function is total (never panics) and idempotent for
// arbitrary input.
func normalizeHost(hostHeader string) string {
	host := hostHeader
	switch {
	case strings.HasPrefix(host, "["):
		if j := strings.IndexByte(host, ']'); j >= 0 {
			content := host[1:j]
			rest := host[j+1:]
			switch {
			case rest == "":
				host = stripPort(content)
			case len(rest) > 1 && rest[0] == ':' && isDigits(rest[1:]):
				host = stripPort(content)
			}
		}
	default:
		host = stripPort(host)
	}
	return strings.ToLower(host)
}

// stripPort removes a trailing ":digits" suffix from host, but only when
// what remains (the candidate host) is itself non-empty and colon-free —
// i.e. host reads unambiguously as "host:port" rather than raw content
// that happens to contain a colon (an IPv6 literal, or bracket content a
// caller already unwrapped). stripPort is idempotent on its own output:
// once applied, the result contains no colon that could trigger a further
// strip, so stripPort(stripPort(x)) == stripPort(x) for all x. This is
// what keeps normalizeHost idempotent even when it feeds bracket content
// (which may itself look like "host:port", e.g. "0:0" from "[0:0]")
// through this same function.
func stripPort(host string) string {
	idx := strings.LastIndexByte(host, ':')
	if idx < 0 {
		return host
	}
	candidate := host[:idx]
	portPart := host[idx+1:]
	if candidate != "" && isDigits(portPart) && !strings.Contains(candidate, ":") {
		return candidate
	}
	return host
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// siteLabel reports whether host is exactly one DNS label below
// baseDomain (a "site host"), returning that label. host must already be
// normalized. Multi-label hosts (a.b.<baseDomain>), the apex itself, and
// unrelated hosts all return ok=false.
func siteLabel(host, baseDomain string) (slug string, ok bool) {
	suffix := "." + baseDomain
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	label := strings.TrimSuffix(host, suffix)
	if label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}

// routeClass classifies a request's Host for the HTTP request histogram:
// the apex host is "app", everything else (site host, multi-label,
// unknown) is "site". "admin" is reserved for CARD-008/009. See ADR-0002.
func routeClass(cfg *config.Config, r *http.Request) string {
	host := normalizeHost(r.Host)
	if host == cfg.BaseDomain {
		return "app"
	}
	return "site"
}
