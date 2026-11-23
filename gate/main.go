// Command gate serves a home page for a Raspberry Pi: a launcher card per
// app, so you open one address instead of remembering ports.
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
	"strconv"
	"syscall"
	"time"

	"gate/internal/config"
	"gate/internal/monitor"
	"gate/internal/server"
	"gate/web"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "path to the configuration file")
	showVersion := flag.Bool("version", false, "print the version and exit")
	checkOnly := flag.Bool("check", false, "validate the configuration file and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *checkOnly {
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s is valid: %d apps, port %d\n", *configPath, len(cfg.Apps), cfg.Port)
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(*configPath); err != nil {
		slog.Error("gate stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mon := monitor.New(cfg)
	go mon.Run(ctx)

	address := net.JoinHostPort(os.Getenv("LISTEN_ADDRESS"), strconv.Itoa(cfg.Port))
	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.Handler(mon, web.Assets),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serveErrors := make(chan error, 1)
	go func() {
		slog.Info("gate listening", "address", address, "apps", len(cfg.Apps), "config", configPath, "version", version)
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
