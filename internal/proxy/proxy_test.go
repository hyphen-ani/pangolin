package proxy

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestProxy(t *testing.T, upstream string) *httptest.Server {
	return newTestProxyWith(t, upstream, "", false)
}

func newTestProxyWith(t *testing.T, upstream, strip string, preserveHost bool) *httptest.Server {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	fwd := NewForwarder(NewTransport(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	dst := Destination{Target: u, StripPrefix: strip, PreserveHost: preserveHost}

	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fwd.Forward(w, r, dst)
	}))
	t.Cleanup(p.Close)
	return p
}

func TestRewritePathAndHost(t *testing.T) {
	cases := []struct {
		name, base, strip, reqPath, wantPath string
		preserveHost                         bool
	}{
		{"no strip", "/api", "", "/users/42", "/api/users/42", false},
		{"strip prefix", "/api", "/users", "/users/42", "/api/42", false},
		{"strip whole path keeps base", "/api", "/users", "/users", "/api", false},
		{"strip whole path, no base", "", "/users", "/users", "/", false},
		{"preserve host", "", "", "/x", "/x", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			type seen struct{ path, host string }
			ch := make(chan seen, 1)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ch <- seen{r.URL.Path, r.Host}
			}))
			defer up.Close()

			p := newTestProxyWith(t, up.URL+c.base, c.strip, c.preserveHost)
			res, err := http.Get(p.URL + c.reqPath)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()

			got := <-ch
			if got.path != c.wantPath {
				t.Errorf("upstream path = %q, want %q", got.path, c.wantPath)
			}
			wantHost := strings.TrimPrefix(up.URL, "http://")
			if c.preserveHost {
				wantHost = strings.TrimPrefix(p.URL, "http://")
			}
			if got.host != wantHost {
				t.Errorf("upstream Host = %q, want %q", got.host, wantHost)
			}
		})
	}
}

func TestProxyForwardsRequestAndResponse(t *testing.T) {
	gotCh := make(chan *http.Request, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCh <- r.Clone(r.Context())
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, "hello")
	}))
	defer up.Close()

	p := newTestProxy(t, up.URL+"/base")

	req, _ := http.NewRequest(http.MethodGet, p.URL+"/items?id=7", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", res.StatusCode)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want hello", body)
	}
	if res.Header.Get("X-Upstream") != "yes" {
		t.Error("upstream response header not copied")
	}

	got := <-gotCh
	if got.URL.Path != "/base/items" {
		t.Errorf("upstream path = %q, want /base/items", got.URL.Path)
	}
	if got.URL.RawQuery != "id=7" {
		t.Errorf("upstream query = %q, want id=7", got.URL.RawQuery)
	}
	if xff := got.Header.Get("X-Forwarded-For"); xff != "203.0.113.9, 127.0.0.1" {
		t.Errorf("X-Forwarded-For = %q", xff)
	}
	if got.Header.Get("X-Forwarded-Proto") != "http" {
		t.Error("X-Forwarded-Proto not set")
	}
}

func TestProxyUpstreamDownReturns502(t *testing.T) {
	// Grab a free port, then close it so nothing is listening there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	p := newTestProxy(t, "http://"+addr)
	res, err := http.Get(p.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestRemoveHopHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "keep-alive, X-Custom-Hop")
	h.Set("X-Custom-Hop", "1")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Transfer-Encoding", "chunked")
	h.Set("X-Keep-Me", "yes")

	removeHopHeaders(h)

	for _, k := range []string{"Connection", "X-Custom-Hop", "Keep-Alive", "Transfer-Encoding"} {
		if h.Get(k) != "" {
			t.Errorf("%s should have been removed", k)
		}
	}
	if h.Get("X-Keep-Me") != "yes" {
		t.Error("end-to-end header was removed")
	}
}

func TestJoinURLPath(t *testing.T) {
	cases := []struct{ base, req, want string }{
		{"", "/a", "/a"},
		{"/", "/a", "/a"},
		{"/api", "/a", "/api/a"},
		{"/api/", "/a", "/api/a"},
		{"/api", "a", "/api/a"},
	}
	for _, c := range cases {
		got, _ := joinURLPath(&url.URL{Path: c.base}, &url.URL{Path: c.req})
		if got != c.want {
			t.Errorf("join(%q, %q) = %q, want %q", c.base, c.req, got, c.want)
		}
	}
}
