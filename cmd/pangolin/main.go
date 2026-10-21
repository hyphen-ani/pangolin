package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyphen-ani/pangolin/internal/config"
	"github.com/hyphen-ani/pangolin/internal/gateway"
	"github.com/hyphen-ani/pangolin/internal/proxy"
	"github.com/hyphen-ani/pangolin/internal/server"
)

const usageText = `Usage: pangolin <command> [flags]

Commands:
  run        start the gateway
  validate   check a config file and exit (non-zero on errors)

Run "pangolin <command> -h" for a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}

	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "run":
		err = runCmd(args)
	case "validate":
		err = validateCmd(args)
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}

	if errors.Is(err, flag.ErrHelp) {
		return // "-h" already printed the flag help
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func validateCmd(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	path := fs.String("config", "pangolin.yaml", "path to the config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}

	routes := 0
	for _, s := range cfg.Services {
		routes += len(s.Routes)
	}
	fmt.Printf("%s: OK (%d services, %d routes)\n", *path, len(cfg.Services), routes)
	return nil
}

// runCmd still proxies to a single --upstream; Step 3 switches it to --config.
func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", "pangolin.yaml", "path to the config file")
	listen := fs.String("listen", ":8080", "proxy listen address")
	drain := fs.Duration("drain-timeout", 30*time.Second, "max time to finish in-flight requests on shutdown")
	logLevel := fs.String("log-level", "info", "debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := newLogger(*logLevel)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	fwd := proxy.NewForwarder(proxy.NewTransport(), logger)
	gw, err := gateway.New(cfg, fwd, logger)
	if err != nil {
		return fmt.Errorf("build gateway: %w", err)
	}
	logger.Info("config loaded", "path", *cfgPath, "services", len(cfg.Services))

	srv := server.New(*listen, gw, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx, *drain)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
