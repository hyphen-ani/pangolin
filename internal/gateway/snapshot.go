package gateway

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/hyphen-ani/pangolin/internal/balancer"
	"github.com/hyphen-ani/pangolin/internal/config"
	"github.com/hyphen-ani/pangolin/internal/router"
)

type Snapshot struct {
	Version  uint64
	Hash     string
	LoadedAt time.Time
	Services int
	Routes   int

	router   *router.Router
	services map[string]*service
}

type service struct {
	name     string
	targets  []*balancer.Target
	balancer balancer.Balancer
}

type stateFunc func(key string) *balancer.State

func buildSnapshot(cfg *config.Config, statefor stateFunc) (*Snapshot, error) {
	rt, err := router.New(cfg)
	if err != nil {
		return nil, err
	}

	snap := &Snapshot{
		router:   rt,
		services: make(map[string]*service, len(cfg.Services)),
		Services: len(cfg.Services),
	}

	for _, s := range cfg.Services {
		snap.Routes += len(s.Routes)

		targets := make([]*balancer.Target, 0, len(s.Upstream.Targets))
		for _, tc := range s.Upstream.Targets {
			u := &url.URL{
				Scheme: s.Upstream.Scheme,
				Host:   net.JoinHostPort(tc.Host, strconv.Itoa(tc.Port)),
				Path:   s.Path,
			}

			key := s.Name + "|" + u.Scheme + "://" + u.Host
			targets = append(targets, balancer.NewTarget(u, *tc.Weight, statefor(key)))
		}

		b, err := balancer.New(s.Upstream.Algorithm, targets)
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", s.Name, err)
		}
		snap.services[s.Name] = &service{
			name:     s.Name,
			targets:  targets,
			balancer: b,
		}
	}
	return snap, nil
}

func Validate(cfg *config.Config) error {
	_, err := buildSnapshot(cfg, func(key string) *balancer.State {
		return balancer.NewState()
	})
	return err
}
