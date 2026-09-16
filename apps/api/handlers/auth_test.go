package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/apperr"
)

func TestAuth(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"login", http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"correct horse battery"}`, func(f *fixture) {
			f.auth.EXPECT().Login(gomock.Any(), "ada@example.com", "correct horse battery").
				Return(&models.TokenPair{AccessToken: "a", RefreshToken: "r", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}, nil)
		}, http.StatusOK, "", false},
		{"login invalid credentials", http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"wrong"}`, func(f *fixture) {
			f.auth.EXPECT().Login(gomock.Any(), "ada@example.com", "wrong").Return(nil, apperr.NewUnauthorized("invalid email or password", nil))
		}, http.StatusUnauthorized, apperr.CodeUnauthorized, false},
		{"refresh", http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"r"}`, func(f *fixture) {
			f.auth.EXPECT().Refresh(gomock.Any(), "r").
				Return(&models.TokenPair{AccessToken: "a2", RefreshToken: "r2", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}, nil)
		}, http.StatusOK, "", false},
		{"logout", http.MethodPost, "/v1/auth/logout", `{"refreshToken":"r"}`, func(f *fixture) {
			f.auth.EXPECT().Logout(gomock.Any(), "r").Return(nil)
		}, http.StatusNoContent, "", false},
		{"logout all", http.MethodPost, "/v1/auth/logout-all", "", func(f *fixture) {
			f.auth.EXPECT().LogoutAll(gomock.Any(), ada.ID).Return(nil)
		}, http.StatusNoContent, "", true},
	})
}
