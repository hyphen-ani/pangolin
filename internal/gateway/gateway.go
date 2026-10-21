package gateway

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hyphen-ani/pangolin/internal/config"
	"github.com/hyphen-ani/pangolin/internal/proxy"
	"github.com/hyphen-ani/pangolin/internal/router"
)

type service struct {
	name   string
	target *url.URL
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
		t := firstActiveTarget(s.Upstream.Targets)
		services[s.Name] = &service{
			name: s.Name,
			target: &url.URL{
				Scheme: s.Upstream.Scheme,
				Host:   net.JoinHostPort(t.Host, strconv.Itoa(t.Port)),
				Path:   s.Path,
			},
		}
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
	dst := proxy.Destination{
		Target:       svc.target,
		PreserveHost: m.Route.PreserveHost}

	if m.Route.StripPath {
		dst.StripPrefix = m.MatchedPrefix
	}

	g.logger.Debug("routed", "route", m.Route.Name, "service", svc.name, "path", r.URL.Path)
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
