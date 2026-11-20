package balancer

import (
	"net/url"
	"sync/atomic"
)

type Target struct {
	Addr   string
	URL    *url.URL
	Weight int
	state  *State
}

type State struct {
	inflight atomic.Int64
	healthy  atomic.Bool
}

func NewState() *State {
	s := &State{}
	s.healthy.Store(true)
	return s
}

func NewTarget(u *url.URL, weight int, st *State) *Target {
	if st == nil {
		st = NewState()
	}
	return &Target{Addr: u.Host, URL: u, Weight: weight, state: st}
}

func (t *Target) Healthy() bool {
	return t.state.healthy.Load()
}

func (t *Target) SetHealthy(ok bool) {
	t.state.healthy.Store(ok)
}

func (t *Target) InFlight() int64 {
	return t.state.inflight.Load()
}

func (t *Target) Acquire() {
	t.state.inflight.Add(1)
}

func (t *Target) Release() {
	t.state.inflight.Add(-1)
}

func (t *Target) eligible() bool {
	return t.Weight > 0 && t.Healthy()
}
