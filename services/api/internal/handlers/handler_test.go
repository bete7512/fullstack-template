package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/httpx"
	"github.com/bete7512/scaffold/pkg/logger"
	"github.com/bete7512/scaffold/services/api/gen/openapi"
	"github.com/bete7512/scaffold/services/api/internal/handlers"
	"github.com/bete7512/scaffold/services/api/internal/models"
	"github.com/bete7512/scaffold/services/api/internal/repos"
	"github.com/bete7512/scaffold/services/api/internal/services/service_mocks"
)

var ada = models.User{
	ID:        1,
	Email:     "ada@example.com",
	Name:      "Ada",
	CreatedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	UpdatedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

type fixture struct {
	users   *service_mocks.MockUserService
	auth    *service_mocks.MockAuthService
	dbErr   error
	handler http.Handler
	logs    bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{users: service_mocks.NewMockUserService(ctrl), auth: service_mocks.NewMockAuthService(ctrl)}
	f.auth.EXPECT().Authenticate(gomock.Any(), "good").Return(auth.Claims{UserID: ada.ID}, nil).AnyTimes()
	l := logger.New(logger.Options{Level: "info", Format: "json", Writer: &f.logs})
	h, err := handlers.New(handlers.Deps{
		Users:  f.users,
		Auth:   f.auth,
		DB:     pingerFunc(func(context.Context) error { return f.dbErr }),
		Logger: l,
	})
	require.NoError(t, err)
	f.handler, err = handlers.NewRouter(h, handlers.RouterOptions{Logger: l, MaxBodyBytes: 1 << 20})
	require.NoError(t, err)
	return f
}

// do sends a request authenticated with the "good" token.
func (f *fixture) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer good")
	return f.send(req)
}

func (f *fixture) send(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func TestEndpoints(t *testing.T) {
	id := strconv.FormatInt(ada.ID, 10)
	createBody := `{"email":"ada@example.com","name":"Ada","password":"correct horse battery"}`

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		setup      func(f *fixture)
		wantStatus int
		wantError  string
	}{
		{"create", http.MethodPost, "/v1/users", createBody, func(f *fixture) {
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), "correct horse battery").
				DoAndReturn(func(_ context.Context, u *models.User, _ string) error { *u = ada; return nil })
		}, http.StatusCreated, ""},
		{"create validation", http.MethodPost, "/v1/users", createBody, func(f *fixture) {
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(apperr.NewValidation(map[string]string{"password": "too short"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation},
		{"create conflict", http.MethodPost, "/v1/users", createBody, func(f *fixture) {
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(apperr.NewConflict("email already registered", nil))
		}, http.StatusConflict, apperr.CodeConflict},
		{"create malformed json", http.MethodPost, "/v1/users", `{"email":`, nil, http.StatusBadRequest, apperr.CodeBadRequest},
		{"create unknown field", http.MethodPost, "/v1/users", `{"extra":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest},

		{"get", http.MethodGet, "/v1/users/" + id, "", func(f *fixture) {
			f.users.EXPECT().GetUser(gomock.Any(), ada.ID).Return(&ada, nil)
		}, http.StatusOK, ""},
		{"get bad id", http.MethodGet, "/v1/users/nope", "", nil, http.StatusBadRequest, apperr.CodeBadRequest},
		{"get not found", http.MethodGet, "/v1/users/" + id, "", func(f *fixture) {
			f.users.EXPECT().GetUser(gomock.Any(), ada.ID).Return(nil, apperr.NewNotFound("user", nil))
		}, http.StatusNotFound, apperr.CodeNotFound},

		{"list", http.MethodGet, "/v1/users", "", func(f *fixture) {
			f.users.EXPECT().ListUsers(gomock.Any(), repos.ListUsersOpts{Limit: 20}).Return(nil, 0, nil)
		}, http.StatusOK, ""},
		{"list with params", http.MethodGet, "/v1/users?limit=5&offset=10&search=ada", "", func(f *fixture) {
			f.users.EXPECT().ListUsers(gomock.Any(), repos.ListUsersOpts{Limit: 5, Offset: 10, Search: "ada"}).Return(nil, 0, nil)
		}, http.StatusOK, ""},
		{"list sorted", http.MethodGet, "/v1/users?sort_by=email&sort_dir=desc", "", func(f *fixture) {
			f.users.EXPECT().ListUsers(gomock.Any(), repos.ListUsersOpts{Limit: 20, SortBy: "email", SortDir: "desc"}).Return(nil, 0, nil)
		}, http.StatusOK, ""},
		{"list bad limit", http.MethodGet, "/v1/users?limit=abc", "", nil, http.StatusBadRequest, apperr.CodeBadRequest},

		{"update", http.MethodPatch, "/v1/users/" + id, `{"name":"Ada King"}`, func(f *fixture) {
			f.users.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).Return(nil)
		}, http.StatusOK, ""},
		{"update not found", http.MethodPatch, "/v1/users/" + id, `{"name":"Ada King"}`, func(f *fixture) {
			f.users.EXPECT().UpdateUser(gomock.Any(), gomock.Any()).Return(apperr.NewNotFound("user", nil))
		}, http.StatusNotFound, apperr.CodeNotFound},

		{"delete", http.MethodDelete, "/v1/users/" + id, "", func(f *fixture) {
			f.users.EXPECT().DeleteUser(gomock.Any(), ada.ID).Return(nil)
		}, http.StatusNoContent, ""},

		{"change password", http.MethodPut, "/v1/users/" + id + "/password", `{"currentPassword":"old password 12","newPassword":"new password 12"}`, func(f *fixture) {
			f.users.EXPECT().ChangePassword(gomock.Any(), ada.ID, "old password 12", "new password 12").Return(nil)
		}, http.StatusNoContent, ""},
		{"change password wrong current", http.MethodPut, "/v1/users/" + id + "/password", `{"currentPassword":"x","newPassword":"new password 12"}`, func(f *fixture) {
			f.users.EXPECT().ChangePassword(gomock.Any(), ada.ID, gomock.Any(), gomock.Any()).
				Return(apperr.NewValidation(map[string]string{"currentPassword": "is incorrect"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation},

		{"me", http.MethodGet, "/v1/me", "", func(f *fixture) {
			f.users.EXPECT().GetUser(gomock.Any(), ada.ID).Return(&ada, nil)
		}, http.StatusOK, ""},

		{"login", http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"correct horse battery"}`, func(f *fixture) {
			f.auth.EXPECT().Login(gomock.Any(), "ada@example.com", "correct horse battery").
				Return(&models.TokenPair{AccessToken: "a", RefreshToken: "r", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}, nil)
		}, http.StatusOK, ""},
		{"login invalid credentials", http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"wrong"}`, func(f *fixture) {
			f.auth.EXPECT().Login(gomock.Any(), "ada@example.com", "wrong").Return(nil, apperr.NewUnauthorized("invalid email or password", nil))
		}, http.StatusUnauthorized, apperr.CodeUnauthorized},
		{"refresh", http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"r"}`, func(f *fixture) {
			f.auth.EXPECT().Refresh(gomock.Any(), "r").
				Return(&models.TokenPair{AccessToken: "a2", RefreshToken: "r2", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}, nil)
		}, http.StatusOK, ""},
		{"logout", http.MethodPost, "/v1/auth/logout", `{"refreshToken":"r"}`, func(f *fixture) {
			f.auth.EXPECT().Logout(gomock.Any(), "r").Return(nil)
		}, http.StatusNoContent, ""},
		{"logout all", http.MethodPost, "/v1/auth/logout-all", "", func(f *fixture) {
			f.auth.EXPECT().LogoutAll(gomock.Any(), ada.ID).Return(nil)
		}, http.StatusNoContent, ""},
		{"healthz", http.MethodGet, "/healthz", "", nil, http.StatusOK, ""},
		{"readyz", http.MethodGet, "/readyz", "", nil, http.StatusOK, ""},
		{"readyz db down", http.MethodGet, "/readyz", "", func(f *fixture) { f.dbErr = errors.New("refused") }, http.StatusServiceUnavailable, apperr.CodeUnavailable},
		{"unknown route", http.MethodGet, "/nope", "", nil, http.StatusNotFound, apperr.CodeNotFound},
		{"openapi", http.MethodGet, "/openapi.json", "", nil, http.StatusOK, ""},
		{"docs", http.MethodGet, "/docs", "", nil, http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			rec := f.do(tt.method, tt.path, tt.body)

			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantError != "" {
				var body httpx.ErrorResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tt.wantError, body.Error)
				assert.NotEmpty(t, body.RequestID)
			}
		})
	}
}

func TestListUsersResponse(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().ListUsers(gomock.Any(), repos.ListUsersOpts{Limit: 20}).
		Return([]models.UserForList{{ID: ada.ID, Email: ada.Email, Name: ada.Name}}, 7, nil)

	rec := f.do(http.MethodGet, "/v1/users", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var page openapi.UserList
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	assert.Equal(t, 7, page.Total)
	require.Len(t, page.Items, 1)
	assert.Equal(t, ada.Email, page.Items[0].Email)
}

func TestInternalErrorIsLoggedNotLeaked(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().GetUser(gomock.Any(), ada.ID).Return(nil, apperr.NewInternal(errors.New("pg: connection refused")))

	rec := f.do(http.MethodGet, "/v1/users/"+strconv.FormatInt(ada.ID, 10), "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "connection refused")

	var line map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(f.logs.Bytes()), &line))
	assert.Equal(t, "ERROR", line["level"])
	assert.Equal(t, "GET /v1/users/{id}", line["route"])
	assert.Contains(t, line["error"], "connection refused")
}

func TestAuthentication(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		authorization string
		cookie        string
		setup         func(f *fixture)
		wantStatus    int
	}{
		{name: "protected route without token", path: "/v1/me", wantStatus: http.StatusUnauthorized},
		{
			name: "invalid token", path: "/v1/me", authorization: "Bearer bad", wantStatus: http.StatusUnauthorized,
			setup: func(f *fixture) {
				f.auth.EXPECT().Authenticate(gomock.Any(), "bad").Return(auth.Claims{}, apperr.NewUnauthorized("invalid or expired token", nil))
			},
		},
		{
			name: "token from cookie", path: "/v1/me", cookie: "good", wantStatus: http.StatusOK,
			setup: func(f *fixture) { f.users.EXPECT().GetUser(gomock.Any(), ada.ID).Return(&ada, nil) },
		},
		{name: "public route without token", path: "/healthz", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "access_token", Value: tt.cookie})
			}

			rec := f.send(req)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}
