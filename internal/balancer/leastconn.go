package balancer

import (
	"sync/atomic"
)

type leastConn struct {
	targets []*Target
	next    atomic.Uint64
}

func newLeastConn(targets []*Target) *leastConn {
	return &leastConn{
		targets: targets,
	}
}

func (b *leastConn) Next() (*Target, error) {
	n := len(b.targets)
	start := int(b.next.Add(1) % uint64(n))

	var best *Target
	var bestLoad int64
	
	for i := 0; i < n; i++ {
		t := b.targets[(start+i)%n]
		if !t.eligible() {
			continue
		}
		load := t.InFlight()
		if best == nil || load*int64(best.Weight) < bestLoad*int64(t.Weight) {
			best, bestLoad = t, load
		}
	}

	if best == nil {
		return nil, ErrNoTarget
	}

	return best, nil
}
