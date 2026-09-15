// Command api runs the HTTP API, or database migrations with -m migrate.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/httpx"
	"github.com/bete7512/scaffold/pkg/logger"
	"github.com/bete7512/scaffold/services/api/internal/app"
	"github.com/bete7512/scaffold/services/api/internal/config"
	"github.com/bete7512/scaffold/services/api/internal/handlers"
	"github.com/bete7512/scaffold/services/api/migrations"
)

func main() {
	mode := flag.String("m", "server", "run mode: server | migrate")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx, *mode, flag.Args())
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode string, args []string) error {
	if mode != "server" && mode != "migrate" {
		return fmt.Errorf("unknown mode %q (want server or migrate)", mode)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(logger.Options{
		Level:   cfg.Log.Level,
		Format:  cfg.Log.Format,
		Service: cfg.App.Service,
		Version: cfg.App.Version,
		Env:     cfg.App.Env,
	})
	slog.SetDefault(log)
	log.InfoContext(ctx, "config loaded", "config", cfg)

	if mode == "migrate" {
		return migrate(ctx, cfg, log, args)
	}
	return serve(ctx, cfg, log)
}

func migrate(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	pool, err := db.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	return migrations.New(log).Run(ctx, pool, args)
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	core, err := app.NewCore(ctx, app.Deps{Config: cfg, Logger: log})
	if err != nil {
		return err
	}
	defer core.Close()

	h, err := handlers.New(handlers.Deps{Users: core.Users, Auth: core.Auth, DB: core.Pool, Logger: log})
	if err != nil {
		return err
	}
	router, err := handlers.NewRouter(h, handlers.RouterOptions{Logger: log, MaxBodyBytes: cfg.HTTP.MaxBodyBytes})
	if err != nil {
		return err
	}

	return httpx.NewServer(router, httpx.Options{
		Addr:              cfg.HTTP.Addr,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ShutdownTimeout:   cfg.HTTP.ShutdownTimeout,
		Logger:            log,
	}).Run(ctx)
}
