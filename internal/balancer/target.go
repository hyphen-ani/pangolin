package balancer

import (
	"net/url"
	"sync/atomic"
)

type Target struct {
	Addr string
	URL *url.URL
	Weight int

	inflight atomic.Int64
	healthy atomic.Bool
}

func NewTarget(u *url.URL, weight int)  *Target {
	t := &Target{Addr: u.Host, URL: u, Weight: weight}
	t.healthy.Store(true)
	return t
}

func (t *Target) Healthy() bool {
	return t.healthy.Load()
}

func (t *Target) SetHealthy(ok bool) {
	t.healthy.Store(ok)
}

func (t *Target) InFlight() int64 {
	return t.inflight.Load()
}

func (t *Target) Acquire() {
	t.inflight.Add(1)
}

func (t *Target) Release() {
	t.inflight.Add(-1)
}

func (t *Target) eligible() bool {
	return t.Weight > 0 && t.Healthy()
}
