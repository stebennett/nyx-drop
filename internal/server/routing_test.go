package server

import (
	"net/http"
	"testing"

	"nyx-drop/internal/config"
)

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Slug.Example.COM:8080", "slug.example.com"},
		{"slug.example.com", "slug.example.com"}, // no-port passthrough
		{"", ""},
		{"EXAMPLE.COM", "example.com"},
		{"example.com:443", "example.com"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := normalizeHost(c.in); got != c.want {
				t.Errorf("normalizeHost(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func FuzzNormalizeHost(f *testing.F) {
	seeds := []string{"Slug.Example.COM:8080", "example.com", "", "a:b:c", "[::1]:8080"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := normalizeHost(s)
		again := normalizeHost(got)
		if got != again {
			t.Fatalf("normalizeHost not idempotent: normalizeHost(%q) = %q, normalizeHost(that) = %q", s, got, again)
		}
	})
}

func TestSiteLabel(t *testing.T) {
	const baseDomain = "sites.nyxhub.net"
	cases := []struct {
		host     string
		wantSlug string
		wantOK   bool
	}{
		{"trusty-tahr.sites.nyxhub.net", "trusty-tahr", true},
		{"sites.nyxhub.net", "", false},          // apex
		{"a.b.sites.nyxhub.net", "", false},      // multi-label
		{"unrelated.example.com", "", false},     // unrelated host
		{"sites.nyxhub.net.evil.com", "", false}, // suffix-but-not-subdomain
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			slug, ok := siteLabel(c.host, baseDomain)
			if ok != c.wantOK || slug != c.wantSlug {
				t.Errorf("siteLabel(%q, %q) = (%q, %v), want (%q, %v)", c.host, baseDomain, slug, ok, c.wantSlug, c.wantOK)
			}
		})
	}
}

func TestRouteClass(t *testing.T) {
	cfg := &config.Config{BaseDomain: "sites.nyxhub.net"}
	cases := []struct {
		name string
		host string
		want string
	}{
		{"apex", "sites.nyxhub.net", "app"},
		{"site host", "trusty-tahr.sites.nyxhub.net", "site"},
		{"unknown host", "unrelated.example.com", "site"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Host = c.host
			if got := routeClass(cfg, r); got != c.want {
				t.Errorf("routeClass(%q) = %q, want %q", c.host, got, c.want)
			}
		})
	}
}
