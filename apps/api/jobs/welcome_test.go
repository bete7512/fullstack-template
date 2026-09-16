package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/services/service_mocks"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/events"
)

func TestWelcome(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		setup     func(users *service_mocks.MockUserService)
		wantErr   error
		wantMails int
	}{
		{
			name: "sends the welcome email", payload: `{"user_id":1}`,
			setup: func(users *service_mocks.MockUserService) {
				users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1, Email: "ada@example.com", Name: "Ada"}, nil)
			},
			wantMails: 1,
		},
		{
			name: "user deleted before the job ran", payload: `{"user_id":1}`,
			setup: func(users *service_mocks.MockUserService) {
				users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(nil, apperr.NewNotFound("user", apperr.ErrNotFound))
			},
			wantErr: apperr.ErrNotFound,
		},
		{name: "malformed payload", payload: `nope`, setup: func(*service_mocks.MockUserService) {}, wantErr: errAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, _, users, mail := newJobs(t)
			tt.setup(users)

			err := j.Welcome(context.Background(), events.Event{ID: 7, Type: apievents.UserRegistered, Payload: json.RawMessage(tt.payload)})

			if tt.wantErr != nil {
				require.Error(t, err)
				if !errors.Is(tt.wantErr, errAny) {
					require.ErrorIs(t, err, tt.wantErr)
				}
				return
			}
			require.NoError(t, err)
			require.Len(t, mail.Messages(), tt.wantMails)
			assert.Equal(t, "ada@example.com", mail.Messages()[0].To)
			assert.Contains(t, mail.Messages()[0].Text, "Ada")
		})
	}
}

var errAny = errors.New("any error")
