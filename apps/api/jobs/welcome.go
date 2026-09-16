package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/mailer"
)

// Welcome emails a newly registered user.
func (j *Jobs) Welcome(ctx context.Context, e events.Event) error {
	var p apievents.UserRegisteredPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
	}
	user, err := j.deps.Users.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	return j.deps.Mail.Send(ctx, mailer.Message{
		To:      user.Email,
		Subject: "Welcome to scaffold",
		Text:    fmt.Sprintf("Hi %s,\n\nYour account is ready.\n", user.Name),
	})
}
