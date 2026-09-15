package services_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/services/api/internal/models"
	"github.com/bete7512/scaffold/services/api/internal/repos"
	"github.com/bete7512/scaffold/services/api/internal/repos/repo_mocks"
	"github.com/bete7512/scaffold/services/api/internal/services"
)

const password = "correct horse battery"

type UserServiceSuite struct {
	suite.Suite
	users    *repo_mocks.MockUserRepo
	sessions *repo_mocks.MockSessionRepo
	hasher   auth.PasswordHasher
	svc      services.UserService
}

func TestUserServiceSuite(t *testing.T) {
	suite.Run(t, new(UserServiceSuite))
}

// SetupSubTest gives every case a fresh mock, so unmet or unexpected calls fail that case.
func (s *UserServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.users = repo_mocks.NewMockUserRepo(ctrl)
	s.sessions = repo_mocks.NewMockSessionRepo(ctrl)
	s.hasher = auth.NewArgon2id(auth.Argon2idParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	var err error
	s.svc, err = services.NewUserService(services.UserServiceDeps{Users: s.users, Sessions: s.sessions, Hasher: s.hasher, Logger: slog.New(slog.DiscardHandler)})
	s.Require().NoError(err)
}

// requireErr asserts err is nil when wantStatus is 0, otherwise an AppError with that status and fields.
func (s *UserServiceSuite) requireErr(err error, wantStatus int, wantFields []string) {
	if wantStatus == 0 {
		s.Require().NoError(err)
		return
	}
	var appErr *apperr.AppError
	s.Require().ErrorAs(err, &appErr)
	s.Equal(wantStatus, appErr.Code)
	for _, field := range wantFields {
		s.Contains(appErr.Fields, field)
	}
}

// hashed returns a user whose password is the package password.
func (s *UserServiceSuite) hashed(id int64) *models.User {
	hash, err := s.hasher.Hash(password)
	s.Require().NoError(err)
	return &models.User{ID: id, PasswordHash: &hash}
}

func (s *UserServiceSuite) TestNewUserService() {
	tests := []struct {
		name    string
		deps    services.UserServiceDeps
		wantErr bool
	}{
		{name: "missing deps", deps: services.UserServiceDeps{}, wantErr: true},
		{name: "missing hasher", deps: services.UserServiceDeps{Logger: slog.New(slog.DiscardHandler)}, wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := services.NewUserService(tt.deps)
			s.Equal(tt.wantErr, err != nil)
		})
	}
}

func (s *UserServiceSuite) TestRegister() {
	tests := []struct {
		name       string
		user       models.User
		password   string
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{
			name: "normalizes and hashes", user: models.User{Email: " Ada@Example.com ", Name: " Ada "}, password: password,
			setup: func() {
				s.users.EXPECT().CreateUser(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, u *models.User) error {
					u.ID = 1
					return nil
				})
			},
		},
		{name: "invalid email and short password", user: models.User{Email: "nope"}, password: "short", wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"email", "password"}},
		{name: "name too long", user: models.User{Email: "ada@example.com", Name: strings.Repeat("a", 121)}, password: password, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"name"}},
		{
			name: "email taken", user: models.User{Email: "ada@example.com"}, password: password, wantStatus: http.StatusConflict,
			setup: func() {
				s.users.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: duplicate", apperr.ErrConflict))
			},
		},
		{
			name: "repo failure", user: models.User{Email: "ada@example.com"}, password: password, wantStatus: http.StatusInternalServerError,
			setup: func() {
				s.users.EXPECT().CreateUser(gomock.Any(), gomock.Any()).Return(errors.New("connection refused"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			user := tt.user

			err := s.svc.Register(context.Background(), &user, tt.password)

			s.requireErr(err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus != 0 {
				return
			}
			s.Equal("ada@example.com", user.Email)
			s.Equal("Ada", user.Name)
			ok, err := s.hasher.Verify(tt.password, *user.PasswordHash)
			s.Require().NoError(err)
			s.True(ok)
		})
	}
}

func (s *UserServiceSuite) TestGetUser() {
	id := int64(7)
	tests := []struct {
		name       string
		repoUser   *models.User
		repoErr    error
		wantStatus int
	}{
		{name: "found", repoUser: &models.User{ID: id}},
		{name: "not found", repoErr: apperr.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "repo failure", repoErr: errors.New("connection refused"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.users.EXPECT().GetUser(gomock.Any(), id).Return(tt.repoUser, tt.repoErr)

			user, err := s.svc.GetUser(context.Background(), id)

			s.requireErr(err, tt.wantStatus, nil)
			if tt.wantStatus == 0 {
				s.Equal(id, user.ID)
			}
		})
	}
}

func (s *UserServiceSuite) TestListUsers() {
	valid := repos.ListUsersOpts{Limit: 2, Offset: 4, Search: "ada"}
	page := []models.UserForList{{ID: 1}, {ID: 2}}
	tests := []struct {
		name       string
		opts       repos.ListUsersOpts
		setup      func()
		wantLen    int
		wantTotal  int
		wantStatus int
		wantFields []string
	}{
		{
			name: "returns page and total", opts: valid, wantLen: 2, wantTotal: 9,
			setup: func() {
				s.users.EXPECT().ListUsers(gomock.Any(), valid).Return(page, nil)
				s.users.EXPECT().CountUsers(gomock.Any(), valid).Return(9, nil)
			},
		},
		{name: "invalid limit and offset", opts: repos.ListUsersOpts{Limit: 0, Offset: -1}, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"limit", "offset"}},
		{name: "invalid sort", opts: repos.ListUsersOpts{Limit: 10, SortBy: "password", SortDir: "sideways"}, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"sort_by", "sort_dir"}},
		{
			name: "list failure", opts: valid, wantStatus: http.StatusInternalServerError,
			setup: func() {
				s.users.EXPECT().ListUsers(gomock.Any(), valid).Return(nil, errors.New("connection refused"))
			},
		},
		{
			name: "count failure", opts: valid, wantStatus: http.StatusInternalServerError,
			setup: func() {
				s.users.EXPECT().ListUsers(gomock.Any(), valid).Return(page, nil)
				s.users.EXPECT().CountUsers(gomock.Any(), valid).Return(0, errors.New("connection refused"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}

			users, total, err := s.svc.ListUsers(context.Background(), tt.opts)

			s.requireErr(err, tt.wantStatus, tt.wantFields)
			s.Len(users, tt.wantLen)
			s.Equal(tt.wantTotal, total)
		})
	}
}

func (s *UserServiceSuite) TestUpdateUser() {
	tests := []struct {
		name       string
		userName   string
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{
			name: "trims and saves", userName: " Ada King ",
			setup: func() { s.users.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).Return(nil) },
		},
		{name: "name too long", userName: strings.Repeat("a", 121), wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"name"}},
		{
			name: "not found", userName: "Ada", wantStatus: http.StatusNotFound,
			setup: func() { s.users.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).Return(apperr.ErrNotFound) },
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			user := &models.User{ID: 1, Name: tt.userName}

			err := s.svc.UpdateUser(context.Background(), user)

			s.requireErr(err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 0 {
				s.Equal("Ada King", user.Name)
			}
		})
	}
}

func (s *UserServiceSuite) TestChangePassword() {
	id := int64(7)
	const newPassword = "brand new password"
	tests := []struct {
		name       string
		current    string
		next       string
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{
			name: "changes password", current: password, next: newPassword,
			setup: func() {
				s.users.EXPECT().GetUser(gomock.Any(), id).Return(s.hashed(id), nil)
				s.users.EXPECT().UpdateUserPassword(gomock.Any(), id, gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, hash string) error {
					ok, err := s.hasher.Verify(newPassword, hash)
					s.Require().NoError(err)
					s.True(ok)
					return nil
				})
				s.sessions.EXPECT().RevokeUserSessions(gomock.Any(), id).Return(nil)
			},
		},
		{name: "new password too short", current: password, next: "short", wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"newPassword"}},
		{
			name: "wrong current password", current: "wrong password!", next: newPassword, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"currentPassword"},
			setup: func() { s.users.EXPECT().GetUser(gomock.Any(), id).Return(s.hashed(id), nil) },
		},
		{
			name: "account without password", current: password, next: newPassword, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"currentPassword"},
			setup: func() { s.users.EXPECT().GetUser(gomock.Any(), id).Return(&models.User{ID: id}, nil) },
		},
		{
			name: "user not found", current: password, next: newPassword, wantStatus: http.StatusNotFound,
			setup: func() { s.users.EXPECT().GetUser(gomock.Any(), id).Return(nil, apperr.ErrNotFound) },
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}

			err := s.svc.ChangePassword(context.Background(), id, tt.current, tt.next)

			s.requireErr(err, tt.wantStatus, tt.wantFields)
		})
	}
}

func (s *UserServiceSuite) TestDeleteUser() {
	tests := []struct {
		name       string
		repoErr    error
		wantStatus int
	}{
		{name: "deletes"},
		{name: "not found", repoErr: apperr.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "repo failure", repoErr: errors.New("connection refused"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.users.EXPECT().DeleteUser(gomock.Any(), gomock.Any()).Return(tt.repoErr)

			err := s.svc.DeleteUser(context.Background(), 1)

			s.requireErr(err, tt.wantStatus, nil)
		})
	}
}
