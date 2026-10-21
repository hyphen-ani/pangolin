package proxy

import (
	"net"
	"net/http"
	"time"
)

// NewTransport returns an http.Transport tuned for
// proxying. One Transport holds a pool of keep-alive
// connections per upstream host
func NewTransport() *http.Transport {

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,  // TCP connect timeout.
		KeepAlive: 30 * time.Second, // TCP keep-alive probes on idle conns.
	}

	return &http.Transport{
		Proxy: nil,
		DialContext: dialer.DialContext,
		ForceAttemptHTTP2: true,
		MaxIdleConns: 1024,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,

		DisableCompression: true,
	}

}