package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyphen-ani/pangolin/internal/proxy"
	"github.com/hyphen-ani/pangolin/internal/server"
)

func main() {

	var (
		listen   = flag.String("listen", ":8080", "proxy listen address")
		upstream = flag.String("upstream", "http://127.0.0.1:9000", "upstream base URL")
		drain    = flag.Duration("drain-timeout", 30*time.Second, "max time to finish in-flight requests on shutdown")
		logLevel = flag.String("log-level", "info", "debug, info, warn or error")
	)
	flag.Parse()

	logger := newLogger(*logLevel)
	if err := run(logger, *listen, *upstream, *drain); err != nil {
		logger.Error("pangolin exited with error", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, listen, upstream string, drain time.Duration) error {
	target, err := url.Parse(upstream)
	if err != nil {
		return fmt.Errorf("parse upstream: %w", err)
	}

	if (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return fmt.Errorf("upstream must be an absolute http(s) URL, got %q", upstream)
	}

	handler := proxy.New(target, proxy.NewTransport(), logger)
	srv := server.New(listen, handler, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx, drain)
}

func newLogger(level string) *slog.Logger {

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))

}
