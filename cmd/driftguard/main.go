package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tanvir-glide/driftguard/internal/config"
	"github.com/tanvir-glide/driftguard/internal/monitor"
)

const version = "0.1.0-m1"

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "configs/example.yaml", "path to the rulebook YAML")
	checkOnly := flag.Bool("check", false, "validate the rulebook and exit")
	debounce := flag.Duration("debounce", 500*time.Millisecond, "coalesce bursts of file changes within this window")
	showVersion := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	if *showVersion {
		fmt.Println("DriftGuard", version)

		return 0
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	rb, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load rulebook", "error", err)

		return 1
	}

	if *checkOnly {
		logger.Info("rulebook is valid", "path", *configPath, "resources", len(rb.Resources))

		return 0
	}

	return serve(logger, rb, *debounce)
}

func serve(logger *slog.Logger, rb *config.Rulebook, debounce time.Duration) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths := rb.WatchPaths()

	w, err := monitor.New(paths, debounce)
	if err != nil {
		logger.Error("start monitor", "error", err)

		return 1
	}

	owners := rb.ResourceIDsByPath()
	runErr := make(chan error, 1)

	go func() { runErr <- w.Run(ctx) }()

	logger.Info("daemon started", "version", version, "resources", len(rb.Resources), "watching", paths)

	for ev := range w.Events() {
		logger.Warn("change detected",
			"path", ev.Path,
			"op", ev.Op,
			"resources", owners[ev.Path],
		)
	}

	err = <-runErr
	if err != nil {
		logger.Error("monitor stopped", "error", err)

		return 1
	}

	logger.Info("daemon stopped")

	return 0
}
