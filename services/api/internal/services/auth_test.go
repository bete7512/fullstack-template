package services_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/services/api/internal/models"
	"github.com/bete7512/scaffold/services/api/internal/repos/repo_mocks"
	"github.com/bete7512/scaffold/services/api/internal/services"
)

type AuthServiceSuite struct {
	suite.Suite
	users    *repo_mocks.MockUserRepo
	sessions *repo_mocks.MockSessionRepo
	hasher   auth.PasswordHasher
	tokens   *auth.Tokens
	svc      services.AuthService
}

func TestAuthServiceSuite(t *testing.T) {
	suite.Run(t, new(AuthServiceSuite))
}

func (s *AuthServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.users = repo_mocks.NewMockUserRepo(ctrl)
	s.sessions = repo_mocks.NewMockSessionRepo(ctrl)
	s.hasher = auth.NewArgon2id(auth.Argon2idParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	var err error
	s.tokens, err = auth.NewTokens(bytes.Repeat([]byte{1}, 32), "scaffold", time.Minute)
	s.Require().NoError(err)
	s.svc, err = services.NewAuthService(services.AuthServiceDeps{
		Users: s.users, Sessions: s.sessions, Hasher: s.hasher, Tokens: s.tokens,
		RefreshTokenTTL: time.Hour, Logger: slog.New(slog.DiscardHandler),
	})
	s.Require().NoError(err)
}

func (s *AuthServiceSuite) requireStatus(err error, want int) {
	if want == 0 {
		s.Require().NoError(err)
		return
	}
	var appErr *apperr.AppError
	s.Require().ErrorAs(err, &appErr)
	s.Equal(want, appErr.Code)
}

// user returns a user whose password is the package password.
func (s *AuthServiceSuite) user(tokenVersion int32) *models.User {
	hash, err := s.hasher.Hash(password)
	s.Require().NoError(err)
	return &models.User{ID: 1, Email: "ada@example.com", PasswordHash: &hash, TokenVersion: tokenVersion}
}

func (s *AuthServiceSuite) TestNewAuthService() {
	s.Run("missing deps", func() {
		_, err := services.NewAuthService(services.AuthServiceDeps{})
		s.Require().Error(err)
	})
}

func (s *AuthServiceSuite) TestLogin() {
	tests := []struct {
		name       string
		password   string
		setup      func()
		wantStatus int
	}{
		{
			name: "valid credentials", password: password,
			setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(s.user(0), nil)
				s.sessions.EXPECT().CreateSession(gomock.Any(), gomock.Cond(func(session *models.Session) bool { return session.UserID == 1 })).Return(nil)
			},
		},
		{
			name: "unknown email", password: password, wantStatus: http.StatusUnauthorized,
			setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "wrong password", password: "wrong password!", wantStatus: http.StatusUnauthorized,
			setup: func() { s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(s.user(0), nil) },
		},
		{
			name: "account without password", password: password, wantStatus: http.StatusUnauthorized,
			setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1}, nil)
			},
		},
		{
			name: "repo failure", password: password, wantStatus: http.StatusInternalServerError,
			setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(nil, errors.New("connection refused"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			pair, err := s.svc.Login(context.Background(), " Ada@Example.com ", tt.password)

			s.requireStatus(err, tt.wantStatus)
			if tt.wantStatus != 0 {
				return
			}
			claims, err := s.tokens.Parse(pair.AccessToken)
			s.Require().NoError(err)
			s.Equal(int64(1), claims.UserID)
			s.NotEmpty(pair.RefreshToken)
		})
	}
}

func (s *AuthServiceSuite) TestRefresh() {
	hash := auth.HashRefreshToken("refresh-1")
	past := time.Now().Add(-time.Hour)
	session := func() *models.Session {
		return &models.Session{ID: 5, UserID: 1, RefreshTokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}
	}

	tests := []struct {
		name       string
		setup      func()
		wantStatus int
	}{
		{
			name: "rotates the current token",
			setup: func() {
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(session(), nil)
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(s.user(0), nil)
				s.sessions.EXPECT().RotateRefreshToken(gomock.Any(), gomock.Any(), hash).Return(nil)
			},
		},
		{
			name:       "unknown token",
			setup:      func() { s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(nil, apperr.ErrNotFound) },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "expired session",
			setup: func() {
				expired := session()
				expired.ExpiresAt = past
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(expired, nil)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "revoked session",
			setup: func() {
				revoked := session()
				revoked.RevokedAt = &past
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(revoked, nil)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "reused previous token revokes the session",
			setup: func() {
				rotated := session()
				rotated.RefreshTokenHash = []byte("newer")
				rotated.PreviousTokenHash = hash
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(rotated, nil)
				s.sessions.EXPECT().RevokeSession(gomock.Any(), int64(5)).Return(nil)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "losing a concurrent rotation revokes the session",
			setup: func() {
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(session(), nil)
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(s.user(0), nil)
				s.sessions.EXPECT().RotateRefreshToken(gomock.Any(), gomock.Any(), hash).Return(apperr.ErrNotFound)
				s.sessions.EXPECT().RevokeSession(gomock.Any(), int64(5)).Return(nil)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "deleted user",
			setup: func() {
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(session(), nil)
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(nil, apperr.ErrNotFound)
			},
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			pair, err := s.svc.Refresh(context.Background(), "refresh-1")

			s.requireStatus(err, tt.wantStatus)
			if tt.wantStatus == 0 {
				s.NotEqual("refresh-1", pair.RefreshToken)
				s.NotEmpty(pair.AccessToken)
			}
		})
	}
}

func (s *AuthServiceSuite) TestLogout() {
	hash := auth.HashRefreshToken("refresh-1")
	tests := []struct {
		name       string
		setup      func()
		wantStatus int
	}{
		{
			name: "ends the session",
			setup: func() {
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(&models.Session{ID: 5}, nil)
				s.sessions.EXPECT().RevokeSession(gomock.Any(), int64(5)).Return(nil)
			},
		},
		{
			name:  "unknown token is a no-op",
			setup: func() { s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(nil, apperr.ErrNotFound) },
		},
		{
			name: "repo failure",
			setup: func() {
				s.sessions.EXPECT().FindSessionByToken(gomock.Any(), hash).Return(nil, errors.New("connection refused"))
			},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()
			s.requireStatus(s.svc.Logout(context.Background(), "refresh-1"), tt.wantStatus)
		})
	}
}

func (s *AuthServiceSuite) TestLogoutAll() {
	tests := []struct {
		name       string
		setup      func()
		wantStatus int
	}{
		{
			name: "invalidates tokens and ends every session",
			setup: func() {
				s.users.EXPECT().BumpUserTokenVersion(gomock.Any(), int64(1)).Return(nil)
				s.sessions.EXPECT().RevokeUserSessions(gomock.Any(), int64(1)).Return(nil)
			},
		},
		{
			name:       "unknown user",
			setup:      func() { s.users.EXPECT().BumpUserTokenVersion(gomock.Any(), int64(1)).Return(apperr.ErrNotFound) },
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()
			s.requireStatus(s.svc.LogoutAll(context.Background(), 1), tt.wantStatus)
		})
	}
}

func (s *AuthServiceSuite) TestAuthenticate() {
	tests := []struct {
		name       string
		token      func() string
		setup      func()
		wantStatus int
	}{
		{
			name:  "valid token",
			token: func() string { return s.issue(0) },
			setup: func() { s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(s.user(0), nil) },
		},
		{
			name:       "stale token version",
			token:      func() string { return s.issue(0) },
			setup:      func() { s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(s.user(1), nil) },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "deleted user",
			token:      func() string { return s.issue(0) },
			setup:      func() { s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(nil, apperr.ErrNotFound) },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			token:      func() string { return "garbage" },
			setup:      func() {},
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			claims, err := s.svc.Authenticate(context.Background(), tt.token())

			s.requireStatus(err, tt.wantStatus)
			if tt.wantStatus == 0 {
				s.Equal(int64(1), claims.UserID)
			}
		})
	}
}

func (s *AuthServiceSuite) issue(tokenVersion int32) string {
	token, _, err := s.tokens.Issue(1, tokenVersion)
	s.Require().NoError(err)
	return token
}
