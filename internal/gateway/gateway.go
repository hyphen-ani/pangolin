package gateway

import (
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyphen-ani/pangolin/internal/balancer"
	"github.com/hyphen-ani/pangolin/internal/config"
	"github.com/hyphen-ani/pangolin/internal/proxy"
)

type Gateway struct {
	current atomic.Pointer[Snapshot]
	fwd     *proxy.Forwarder
	logger  *slog.Logger
	mu      sync.Mutex
	version uint64
	states  map[string]*balancer.State
}

func New(fwd *proxy.Forwarder, logger *slog.Logger) *Gateway {
	return &Gateway{
		fwd:    fwd,
		logger: logger,
		states: map[string]*balancer.State{},
	}
}

func (g *Gateway) Apply(cfg *config.Config, hash string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	start := time.Now()
	used := make(map[string]*balancer.State)
	snap, err := buildSnapshot(cfg, func(key string) *balancer.State {
		st, ok := g.states[key]
		if !ok {
			st = balancer.NewState()
		}
		used[key] = st
		return st
	})
	if err != nil {
		return err
	}

	g.version++
	snap.Version, snap.Hash, snap.LoadedAt = g.version, hash, time.Now()
	g.current.Store(snap)
	g.states = used

	g.logger.Info("config applied",
		"version", snap.Version, "hash", hash,
		"services", snap.Services, "routes", snap.Routes,
		"took", time.Since(start).String())

	return nil
}

func (g *Gateway) Current() *Snapshot {
	return g.current.Load()
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snap := g.current.Load()
	if snap == nil {
		proxy.WriteError(w, http.StatusServiceUnavailable, "gateway has no config loaded")
		return
	}

	m, ok := snap.router.Match(r)
	if !ok {
		proxy.WriteError(w, http.StatusNotFound, "no route matched")
		return
	}

	svc := snap.services[m.Route.Service]
	t, err := svc.balancer.Next()
	if err != nil {
		g.logger.Warn("no upstream available", "service", svc.name, "err", err)
		proxy.WriteError(w, http.StatusServiceUnavailable, "no healthy upstream")
		return
	}

	t.Acquire()
	defer t.Release()

	dst := proxy.Destination{Target: t.URL, PreserveHost: m.Route.PreserveHost}
	if m.Route.StripPath {
		dst.StripPrefix = m.MatchedPrefix
	}

	g.logger.Debug("routed",
		"version", snap.Version, "route", m.Route.Name,
		"service", svc.name, "target", t.Addr, "path", r.URL.Path)
	g.fwd.Forward(w, r, dst)
}
