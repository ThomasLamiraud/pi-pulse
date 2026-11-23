// Command pipulse is a lightweight Raspberry Pi monitoring dashboard: one
// process samples the host every few seconds, keeps 24 hours of history in
// SQLite and serves a live dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pipulse/internal/collector"
	"pipulse/internal/metrics"
	"pipulse/internal/server"
	"pipulse/internal/store"
	"pipulse/web"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("pipulse stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	interval := collector.Interval(os.Getenv("METRIC_INTERVAL"))
	address := net.JoinHostPort(env("LISTEN_ADDRESS", "0.0.0.0"), env("PORT", "3000"))
	databasePath := env("DATABASE_PATH", "pipulse.db")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watched, err := metrics.ParseDatabaseSpecs(os.Getenv("WATCH_DATABASES"))
	if err != nil {
		return err
	}
	databases := metrics.NewDatabaseReader(append([]metrics.DatabaseSpec{{Name: "PiPulse", Path: databasePath}}, watched...))

	db, err := store.Open(databasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	srv := server.New(db, web.Assets, interval, collector.FreshnessThreshold(interval))
	httpServer := &http.Server{
		Addr:              address,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// Request contexts end on shutdown, which closes live dashboard streams.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	go collector.New(metrics.NewReader(metrics.DefaultPaths(), databases), db, srv.Publish).Run(ctx, interval)

	serveErrors := make(chan error, 1)
	go func() {
		slog.Info("pipulse listening", "address", address, "database", databasePath, "version", version)
		serveErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		return err
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdown); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
