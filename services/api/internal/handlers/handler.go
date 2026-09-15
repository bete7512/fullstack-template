// Package handlers implements the generated openapi.ServerInterface.
// Handlers decode requests into generated DTOs, call services, and write
// responses; every error goes through httpx.WriteError.
package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bete7512/scaffold/services/api/gen/openapi"
	"github.com/bete7512/scaffold/services/api/internal/services"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the handler dependencies; all are required.
type Deps struct {
	Users  services.UserService
	Auth   services.AuthService
	DB     Pinger
	Logger *slog.Logger
}

// Handler serves every operation in the OpenAPI spec.
type Handler struct {
	deps Deps
}

var _ openapi.ServerInterface = (*Handler)(nil)

// New returns a Handler or an error if a dependency is missing.
func New(deps Deps) (*Handler, error) {
	if deps.Users == nil || deps.Auth == nil || deps.DB == nil || deps.Logger == nil {
		return nil, errors.New("handlers: Users, Auth, DB and Logger are required")
	}
	return &Handler{deps: deps}, nil
}
