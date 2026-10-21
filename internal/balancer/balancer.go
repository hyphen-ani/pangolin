package balancer

import (
	"errors"
	"fmt"

	"github.com/hyphen-ani/pangolin/internal/config"
)

var ErrNoTarget = errors.New("no healthy upstream target")

type Balancer interface {
	Next() (*Target, error)
}

func New(algorithm string, targets []*Target) (Balancer, error) {

	if len(targets) == 0 {
		return nil, errors.New("balancer needs at least one target")
	}

	switch algorithm {
	case config.AlgoRoundRobin:
		return newRoundRobin(targets), nil
	case config.AlgoLeastConnections:
		return newLeastConn(targets), nil
	default:
		return nil, fmt.Errorf("unknown balancing algorithm %q", algorithm)
	}
}
