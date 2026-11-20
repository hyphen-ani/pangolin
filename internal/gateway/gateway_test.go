package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyphen-ani/pangolin/internal/config"
	"github.com/hyphen-ani/pangolin/internal/proxy"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newGateway() *Gateway {
	return New(proxy.NewForwarder(proxy.NewTransport(), discard()), discard())
}

func newUpstream(t *testing.T, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func testConfig(t *testing.T, upstreamURL string, weight int) *config.Config {
	t.Helper()
	u, _ := url.Parse(upstreamURL)
	host, port, _ := net.SplitHostPort(u.Host)
	y := fmt.Sprintf(`
apiVersion: pangolinhq.dev/v1alpha1
kind: Config
services:
  - name: svc
    upstream:
      targets: [{ host: %s, port: %s, weight: %d }]
    routes: [{ name: all, paths: [/] }]
`, host, port, weight)
	cfg, err := config.Parse(strings.NewReader(y))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNoConfigReturns503(t *testing.T) {
	rec := httptest.NewRecorder()
	newGateway().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestReloadUnderLoadDropsNothing(t *testing.T) {
	a, b := newUpstream(t, "a"), newUpstream(t, "b")
	cfgA, cfgB := testConfig(t, a.URL, 100), testConfig(t, b.URL, 100)

	g := newGateway()
	if err := g.Apply(cfgA, "a"); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(g)
	defer gw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var failed, total atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				total.Add(1)
				res, err := http.Get(gw.URL + "/")
				if err != nil {
					failed.Add(1)
					continue
				}
				body, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode != 200 || (string(body) != "a" && string(body) != "b") {
					failed.Add(1)
				}
			}
		}()
	}

	for i := 0; i < 100; i++ {
		cfg := cfgA
		if i%2 == 1 {
			cfg = cfgB
		}
		if err := g.Apply(cfg, fmt.Sprint(i)); err != nil {
			t.Errorf("apply %d: %v", i, err)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	wg.Wait()

	if n := failed.Load(); n > 0 {
		t.Errorf("%d of %d requests failed during 100 reloads", n, total.Load())
	}
}

func TestFailedApplyKeepsCurrentSnapshot(t *testing.T) {
	a := newUpstream(t, "a")
	g := newGateway()
	if err := g.Apply(testConfig(t, a.URL, 100), "good"); err != nil {
		t.Fatal(err)
	}
	before := g.Current()

	bad := testConfig(t, a.URL, 100)
	r := bad.Services[0].Routes[0]
	r.Name = "all-copy" // identical match criteria: the router rejects it
	bad.Services[0].Routes = append(bad.Services[0].Routes, r)

	if err := g.Apply(bad, "bad"); err == nil {
		t.Fatal("expected conflicting config to be rejected")
	}
	if g.Current() != before {
		t.Error("snapshot changed after a failed apply")
	}
}

func TestTargetStateSurvivesReload(t *testing.T) {
	a := newUpstream(t, "a")
	g := newGateway()
	if err := g.Apply(testConfig(t, a.URL, 100), "v1"); err != nil {
		t.Fatal(err)
	}
	t1 := g.Current().services["svc"].targets[0]
	t1.SetHealthy(false)
	t1.Acquire()

	if err := g.Apply(testConfig(t, a.URL, 50), "v2"); err != nil { // only the weight changes
		t.Fatal(err)
	}
	t2 := g.Current().services["svc"].targets[0]

	if t2 == t1 {
		t.Fatal("expected a new Target for the new snapshot")
	}
	if t2.Weight != 50 {
		t.Errorf("weight = %d, want 50", t2.Weight)
	}
	if t2.Healthy() || t2.InFlight() != 1 {
		t.Error("health and in-flight state were not carried over")
	}
}
