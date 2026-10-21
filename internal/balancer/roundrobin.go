package balancer

import (
	"sync"
)

type roundrobin struct {
	mu      sync.Mutex
	targets []*Target
	current []int
}

func newRoundRobin(targets []*Target) *roundrobin {
	return &roundrobin{
		targets: targets,
		current: make([]int, len(targets)),
	}
}

func (b *roundrobin) Next() (*Target, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	best, total := -1, 0
	for i, t := range b.targets {
		if !t.eligible() {
			b.current[i] = 0
			continue
		}
		b.current[i] += t.Weight
		total += t.Weight
		if best == -1 || b.current[i] > b.current[best] {
			best = i
		}
	}

	if best == -1 {
		return nil, ErrNoTarget
	}

	b.current[best] -= total
	return b.targets[best], nil
}
