// Package app wires the dependencies shared by every binary.
package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/services/api/internal/config"
	"github.com/bete7512/scaffold/services/api/internal/repos"
	"github.com/bete7512/scaffold/services/api/internal/services"
)

// Deps are what NewCore needs.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
}

// Core holds the resources and services shared by the api and the worker.
type Core struct {
	Pool  *pgxpool.Pool
	Users services.UserService
}

// NewCore opens the database pool and builds repos and services.
func NewCore(ctx context.Context, d Deps) (*Core, error) {
	pool, err := db.NewPoolWithRetries(ctx, d.Logger, d.Config.DB)
	if err != nil {
		return nil, err
	}

	userRepo, err := repos.NewUserRepo(pool)
	if err != nil {
		pool.Close()
		return nil, err
	}

	params := auth.DefaultArgon2idParams()
	params.Memory = d.Config.Auth.Argon2MemoryKiB
	params.Iterations = d.Config.Auth.Argon2Iterations
	params.Parallelism = d.Config.Auth.Argon2Parallelism

	users, err := services.NewUserService(services.UserServiceDeps{
		Users:  userRepo,
		Hasher: auth.NewArgon2id(params),
		Logger: d.Logger,
	})
	if err != nil {
		pool.Close()
		return nil, err
	}

	return &Core{Pool: pool, Users: users}, nil
}

// Close releases the database pool.
func (c *Core) Close() {
	c.Pool.Close()
}
