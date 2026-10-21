package balancer

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hyphen-ani/pangolin/internal/config"
)

func targets(weights ...int) []*Target {
	ts := make([]*Target, len(weights))
	for i, w := range weights {
		ts[i] = NewTarget(&url.URL{Scheme: "http", Host: fmt.Sprintf("10.0.0.%d:80", i+1)}, w)
	}
	return ts
}

func mustNew(t *testing.T, algo string, ts []*Target) Balancer {
	t.Helper()
	b, err := New(algo, ts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func pick(t *testing.T, b Balancer) *Target {
	t.Helper()
	tg, err := b.Next()
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func TestRoundRobinSmoothSequence(t *testing.T) {
	ts := targets(5, 1, 1)
	b := mustNew(t, config.AlgoRoundRobin, ts)
	name := map[*Target]string{ts[0]: "a", ts[1]: "b", ts[2]: "c"}

	var got strings.Builder
	for i := 0; i < 7; i++ {
		got.WriteString(name[pick(t, b)])
	}
	if got.String() != "aabacaa" {
		t.Errorf("sequence = %q, want aabacaa", got.String())
	}
}

func TestRoundRobinExactProportionsAndDrain(t *testing.T) {
	ts := targets(100, 50, 0) // third target drained
	b := mustNew(t, config.AlgoRoundRobin, ts)

	counts := map[*Target]int{}
	for i := 0; i < 300; i++ { // two full cycles of 150
		counts[pick(t, b)]++
	}
	if counts[ts[0]] != 200 || counts[ts[1]] != 100 || counts[ts[2]] != 0 {
		t.Errorf("counts = %d/%d/%d, want 200/100/0", counts[ts[0]], counts[ts[1]], counts[ts[2]])
	}
}

func TestSkipsUnhealthyTargets(t *testing.T) {
	for _, algo := range []string{config.AlgoRoundRobin, config.AlgoLeastConnections} {
		t.Run(algo, func(t *testing.T) {
			ts := targets(100, 100)
			b := mustNew(t, algo, ts)

			ts[0].SetHealthy(false)
			for i := 0; i < 10; i++ {
				if pick(t, b) != ts[1] {
					t.Fatal("picked an unhealthy target")
				}
			}

			ts[1].SetHealthy(false)
			if _, err := b.Next(); !errors.Is(err, ErrNoTarget) {
				t.Errorf("err = %v, want ErrNoTarget", err)
			}
		})
	}
}

func TestLeastConnPicksLeastLoaded(t *testing.T) {
	ts := targets(100, 100, 100)
	b := mustNew(t, config.AlgoLeastConnections, ts)
	ts[0].Acquire()
	ts[0].Acquire()
	ts[2].Acquire()

	for i := 0; i < 5; i++ {
		if got := pick(t, b); got != ts[1] {
			t.Fatalf("picked %s, want the idle %s", got.Addr, ts[1].Addr)
		}
	}
}

func TestLeastConnRespectsWeight(t *testing.T) {
	ts := targets(200, 100)
	b := mustNew(t, config.AlgoLeastConnections, ts)

	// 1/200 < 1/100: the heavier target can take more load.
	ts[0].Acquire()
	ts[1].Acquire()
	if pick(t, b) != ts[0] {
		t.Error("want the weight-200 target at equal in-flight")
	}

	// 3/200 > 1/100: now the lighter target is relatively less loaded.
	ts[0].Acquire()
	ts[0].Acquire()
	if pick(t, b) != ts[1] {
		t.Error("want the weight-100 target once the heavy one is busier")
	}
}

func TestLeastConnSpreadsTies(t *testing.T) {
	ts := targets(100, 100, 100)
	b := mustNew(t, config.AlgoLeastConnections, ts)

	counts := map[*Target]int{}
	for i := 0; i < 300; i++ {
		counts[pick(t, b)]++
	}
	for _, tg := range ts {
		if counts[tg] != 100 {
			t.Errorf("%s got %d picks, want 100", tg.Addr, counts[tg])
		}
	}
}

func TestConcurrentNext(t *testing.T) {
	for _, algo := range []string{config.AlgoRoundRobin, config.AlgoLeastConnections} {
		t.Run(algo, func(t *testing.T) {
			b := mustNew(t, algo, targets(3, 2, 1))
			var wg sync.WaitGroup
			for g := 0; g < 8; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 1000; i++ {
						tg, err := b.Next()
						if err != nil {
							t.Error(err)
							return
						}
						tg.Acquire()
						tg.Release()
					}
				}()
			}
			wg.Wait()
		})
	}
}
