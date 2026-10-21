package gateway

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hyphen-ani/pangolin/internal/balancer"
	"github.com/hyphen-ani/pangolin/internal/config"
	"github.com/hyphen-ani/pangolin/internal/proxy"
	"github.com/hyphen-ani/pangolin/internal/router"
)

type service struct {
	name     string
	targets  []*balancer.Target
	balancer balancer.Balancer
}

type Gateway struct {
	router   *router.Router
	services map[string]*service
	fwd      *proxy.Forwarder
	logger   *slog.Logger
}

func New(cfg *config.Config, fwd *proxy.Forwarder, logger *slog.Logger) (*Gateway, error) {
	rt, err := router.New(cfg)
	if err != nil {
		return nil, err
	}

	services := make(map[string]*service, len(cfg.Services))
	for _, s := range cfg.Services {
		targets := make([]*balancer.Target, 0, len(s.Upstream.Targets))
		for _, tc := range s.Upstream.Targets {
			u := &url.URL{
				Scheme: s.Upstream.Scheme,
				Host:   net.JoinHostPort(tc.Host, strconv.Itoa(tc.Port)),
				Path:   s.Path,
			}
			targets = append(targets, balancer.NewTarget(u, *tc.Weight))
		}

		b, err := balancer.New(s.Upstream.Algorithm, targets)
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", s.Name, err)
		}
		services[s.Name] = &service{name: s.Name, targets: targets, balancer: b}
	}

	return &Gateway{router: rt, services: services, fwd: fwd, logger: logger}, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m, ok := g.router.Match(r)
	if !ok {
		proxy.WriteError(w, http.StatusNotFound, "no route matched")
		return
	}

	svc := g.services[m.Route.Service]
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
		"route", m.Route.Name, "service", svc.name, "target", t.Addr, "path", r.URL.Path)
	g.fwd.Forward(w, r, dst)
}

func firstActiveTarget(ts []config.Target) config.Target {
	for _, t := range ts {
		if *t.Weight > 0 {
			return t
		}
	}
	return ts[0]
}
