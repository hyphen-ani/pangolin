package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type Server struct {
	http   *http.Server
	logger *slog.Logger
}

func New(addr string, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		logger: logger,
		http: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second, // slowloris protection
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    64 << 10, // 64 KiB
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		},
	}
}

/*
* Run serves until ctx is cancelled, then stops accepting new connections
and waits up to drain for in-flight requests to finish. *
*/
func (s *Server) Run(ctx context.Context, drain time.Duration) error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.http.Addr, err)
	}
	s.logger.Info("pangolin listening", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(ln) }()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err) // failed before shutdown was asked for
	case <-ctx.Done():
	}

	s.logger.Info("shutting down, draining in-flight requests", "timeout", drain.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	s.logger.Info("shutdown complete")
	return nil
}
