// Command worker runs background jobs.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bete7512/scaffold/pkg/logger"
	"github.com/bete7512/scaffold/services/api/internal/app"
	"github.com/bete7512/scaffold/services/api/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(logger.Options{
		Level:   cfg.Log.Level,
		Format:  cfg.Log.Format,
		Service: "worker",
		Version: cfg.App.Version,
		Env:     cfg.App.Env,
	})
	slog.SetDefault(log)

	core, err := app.NewCore(ctx, app.Deps{Config: cfg, Logger: log})
	if err != nil {
		return err
	}
	defer core.Close()

	log.InfoContext(ctx, "worker started")
	<-ctx.Done()
	log.InfoContext(context.Background(), "worker stopped")
	return nil
}
