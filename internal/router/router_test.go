package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyphen-ani/pangolin/internal/config"
)

const routesYAML = `
apiVersion: pangolinhq.dev/v1alpha1
kind: Config
services:
  - name: svc
    upstream: { targets: [{ host: 127.0.0.1, port: 9000 }] }
    routes:
      - { name: catch-all, paths: [/] }
      - { name: users, paths: [/users] }
      - { name: users-post, paths: [/users], methods: [POST] }
      - { name: users-v2, paths: [/users], headers: { X-Version: [v2] } }
      - { name: user-by-id, paths: ["~^/users/[0-9]+$"] }
      - { name: deep, paths: [/users/admin] }
      - { name: health, paths: ["=/healthz"] }
      - { name: api-host, hosts: [api.example.com], paths: [/users] }
      - { name: wildcard-host, hosts: ["*.example.com"] }
`

func mustRouter(t *testing.T, y string) *Router {
	t.Helper()
	cfg, err := config.Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestMatch(t *testing.T) {
	rt := mustRouter(t, routesYAML)

	cases := []struct {
		name, method, host, path string
		headers                  map[string]string
		wantRoute, wantPrefix    string
	}{
		{"exact path", "GET", "localhost", "/healthz", nil, "health", "/healthz"},
		{"exact does not match sub-path", "GET", "localhost", "/healthz/x", nil, "catch-all", ""},
		{"prefix", "GET", "localhost", "/users", nil, "users", "/users"},
		{"method is more specific", "POST", "localhost", "/users", nil, "users-post", "/users"},
		{"header is more specific", "GET", "localhost", "/users", map[string]string{"X-Version": "v2"}, "users-v2", "/users"},
		{"regex beats prefix", "GET", "localhost", "/users/42", nil, "user-by-id", ""},
		{"regex miss falls back to prefix", "GET", "localhost", "/users/abc", nil, "users", "/users"},
		{"segment boundary", "GET", "localhost", "/usersettings", nil, "catch-all", ""},
		{"longest prefix", "GET", "localhost", "/users/admin/x", nil, "deep", "/users/admin"},
		{"exact host", "GET", "api.example.com", "/users", nil, "api-host", "/users"},
		{"host normalised", "GET", "API.Example.com.:8443", "/users", nil, "api-host", "/users"},
		{"exact host miss falls to wildcard", "GET", "api.example.com", "/other", nil, "wildcard-host", ""},
		{"wildcard host", "GET", "www.example.com", "/users", nil, "wildcard-host", ""},
		{"wildcard excludes bare domain", "GET", "example.com", "/users", nil, "users", "/users"},
		{"host-bound prefix beats host-less regex", "GET", "api.example.com", "/users/42", nil, "api-host", "/users"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "http://"+c.host+c.path, nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			m, ok := rt.Match(req)
			if !ok {
				t.Fatalf("no match, want %q", c.wantRoute)
			}
			if m.Route.Name != c.wantRoute {
				t.Errorf("route = %q, want %q", m.Route.Name, c.wantRoute)
			}
			if m.MatchedPrefix != c.wantPrefix {
				t.Errorf("prefix = %q, want %q", m.MatchedPrefix, c.wantPrefix)
			}
		})
	}
}

func TestNoMatch(t *testing.T) {
	rt := mustRouter(t, `
apiVersion: pangolinhq.dev/v1alpha1
kind: Config
services:
  - name: svc
    upstream: { targets: [{ host: 127.0.0.1, port: 9000 }] }
    routes:
      - { name: only-get, paths: [/a], methods: [GET] }
`)
	for _, req := range []*http.Request{
		httptest.NewRequest("GET", "/b", nil),
		httptest.NewRequest("DELETE", "/a", nil),
	} {
		if m, ok := rt.Match(req); ok {
			t.Errorf("%s %s matched %q, want no match", req.Method, req.URL.Path, m.Route.Name)
		}
	}
}

func TestConflictingRoutesRejected(t *testing.T) {
	cfg, err := config.Parse(strings.NewReader(`
apiVersion: pangolinhq.dev/v1alpha1
kind: Config
services:
  - name: a
    upstream: { targets: [{ host: 127.0.0.1, port: 9000 }] }
    routes: [{ name: r1, paths: [/x], methods: [GET] }]
  - name: b
    upstream: { targets: [{ host: 127.0.0.1, port: 9001 }] }
    routes: [{ name: r2, paths: [/x], methods: [GET] }]
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "identical") {
		t.Errorf("want conflict error, got %v", err)
	}
}
