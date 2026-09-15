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
	Auth  services.AuthService
}

// NewCore opens the database pool and builds repos and services.
func NewCore(ctx context.Context, d Deps) (_ *Core, err error) {
	pool, err := db.NewPoolWithRetries(ctx, d.Logger, d.Config.DB)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			pool.Close()
		}
	}()

	userRepo, err := repos.NewUserRepo(pool)
	if err != nil {
		return nil, err
	}
	sessionRepo, err := repos.NewSessionRepo(pool)
	if err != nil {
		return nil, err
	}

	params := auth.DefaultArgon2idParams()
	params.Memory = d.Config.Auth.Argon2MemoryKiB
	params.Iterations = d.Config.Auth.Argon2Iterations
	params.Parallelism = d.Config.Auth.Argon2Parallelism
	hasher := auth.NewArgon2id(params)

	tokens, err := auth.NewTokens(d.Config.Auth.SigningKeySeed(), d.Config.Auth.Issuer, d.Config.Auth.AccessTokenTTL)
	if err != nil {
		return nil, err
	}

	users, err := services.NewUserService(services.UserServiceDeps{
		Users:    userRepo,
		Sessions: sessionRepo,
		Hasher:   hasher,
		Logger:   d.Logger,
	})
	if err != nil {
		return nil, err
	}
	authService, err := services.NewAuthService(services.AuthServiceDeps{
		Users:           userRepo,
		Sessions:        sessionRepo,
		Hasher:          hasher,
		Tokens:          tokens,
		RefreshTokenTTL: d.Config.Auth.RefreshTokenTTL,
		Logger:          d.Logger,
	})
	if err != nil {
		return nil, err
	}

	return &Core{Pool: pool, Users: users, Auth: authService}, nil
}

// Close releases the database pool.
func (c *Core) Close() {
	c.Pool.Close()
}
