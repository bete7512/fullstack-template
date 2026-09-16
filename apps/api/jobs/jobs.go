// Package jobs holds the worker's event handlers and runs each delivered event once.
package jobs

import (
	"context"
	"errors"
	"log/slog"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/apps/api/services"
	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/mailer"
)

// Handler processes one event.
type Handler func(ctx context.Context, e events.Event) error

// Deps are what the jobs need; all are required.
type Deps struct {
	Events services.EventService
	Users  services.UserService
	Mail   mailer.Mailer
	Logger *slog.Logger
}

// Jobs holds the handlers of the api app and dispatches events to them.
type Jobs struct {
	deps     Deps
	handlers map[string]Handler
}

// New returns the jobs or an error if a dependency is missing.
func New(deps Deps) (*Jobs, error) {
	if deps.Events == nil || deps.Users == nil || deps.Mail == nil || deps.Logger == nil {
		return nil, errors.New("jobs: Events, Users, Mail and Logger are required")
	}
	j := &Jobs{deps: deps}
	j.handlers = map[string]Handler{
		apievents.UserRegistered: j.Welcome,
	}
	return j, nil
}

// Handle is the events.Handler the subscriber calls: unknown types are acked and logged, every other event runs once.
func (j *Jobs) Handle(ctx context.Context, e events.Event) error {
	h, ok := j.handlers[e.Type]
	if !ok {
		j.deps.Logger.WarnContext(ctx, "no handler for event", "type", e.Type, "event_id", e.ID)
		return nil
	}
	fresh, err := j.deps.Events.ProcessOnce(ctx, e.ID, func(ctx context.Context) error {
		return h(ctx, e)
	})
	if err != nil {
		return err
	}
	if !fresh {
		j.deps.Logger.InfoContext(ctx, "event already processed", "type", e.Type, "event_id", e.ID)
	}
	return nil
}
