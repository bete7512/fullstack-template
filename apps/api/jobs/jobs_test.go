package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/apps/api/jobs"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/services/service_mocks"
	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/mailer/memory"
)

// newJobs builds Jobs over mocks; the returned mail records what handlers send.
func newJobs(t *testing.T) (*jobs.Jobs, *service_mocks.MockEventService, *service_mocks.MockUserService, *memory.Mailer) {
	t.Helper()
	ctrl := gomock.NewController(t)
	evs := service_mocks.NewMockEventService(ctrl)
	users := service_mocks.NewMockUserService(ctrl)
	mail := memory.New()
	j, err := jobs.New(jobs.Deps{Events: evs, Users: users, Mail: mail, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return j, evs, users, mail
}

func TestHandle(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		eventType string
		fresh     bool
		userErr   error
		wantMails int
		wantErr   error
	}{
		{name: "runs the handler for a new event", eventType: apievents.UserRegistered, fresh: true, wantMails: 1},
		{name: "skips the handler on a replay", eventType: apievents.UserRegistered, fresh: false},
		{name: "handler error is returned for redelivery", eventType: apievents.UserRegistered, fresh: true, userErr: boom, wantErr: boom},
		{name: "unknown type is acked without touching the service", eventType: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, evs, users, mail := newJobs(t)
			if tt.eventType == apievents.UserRegistered {
				evs.EXPECT().ProcessOnce(gomock.Any(), int64(7), gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ int64, fn func(context.Context) error) (bool, error) {
						if !tt.fresh {
							return false, nil
						}
						if err := fn(ctx); err != nil {
							return false, err
						}
						return true, nil
					})
			}
			if tt.fresh {
				users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1, Email: "ada@example.com", Name: "Ada"}, tt.userErr)
			}

			err := j.Handle(context.Background(), events.Event{ID: 7, Type: tt.eventType, Payload: []byte(`{"user_id":1}`)})

			require.Len(t, mail.Messages(), tt.wantMails)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
