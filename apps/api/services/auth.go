package services

//go:generate mockgen -source=auth.go -destination=service_mocks/mock_auth.go -package=service_mocks

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
)

// AuthService logs users in and out and verifies access tokens.
type AuthService interface {
	Login(ctx context.Context, email, password string) (*models.TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (*models.TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
	LogoutAll(ctx context.Context, userID int64) error
	Authenticate(ctx context.Context, accessToken string) (auth.Claims, error)
}

// AuthServiceDeps are the AuthService dependencies; all are required.
type AuthServiceDeps struct {
	Users           repos.UserRepo
	Sessions        repos.SessionRepo
	Hasher          auth.PasswordHasher
	Tokens          *auth.Tokens
	RefreshTokenTTL time.Duration
	Logger          *slog.Logger
}

type authService struct {
	deps AuthServiceDeps
}

// NewAuthService returns an AuthService or an error if a dependency is missing.
func NewAuthService(deps AuthServiceDeps) (AuthService, error) {
	if deps.Users == nil || deps.Sessions == nil || deps.Hasher == nil || deps.Tokens == nil || deps.Logger == nil || deps.RefreshTokenTTL <= 0 {
		return nil, errors.New("services: Users, Sessions, Hasher, Tokens, RefreshTokenTTL and Logger are required")
	}
	return &authService{deps: deps}, nil
}

func (s *authService) Login(ctx context.Context, email, password string) (*models.TokenPair, error) {
	user, err := s.deps.Users.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, apperr.ErrNotFound) {
		// Do the same argon2 work as a real check so response time does not reveal which emails exist.
		_, _ = s.deps.Hasher.Hash(password)
		return nil, invalidCredentials()
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if !user.HasPassword() {
		return nil, invalidCredentials()
	}
	ok, err := s.deps.Hasher.Verify(password, *user.PasswordHash)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if !ok {
		return nil, invalidCredentials()
	}

	refreshToken, hash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	session := &models.Session{UserID: user.ID, RefreshTokenHash: hash, ExpiresAt: time.Now().Add(s.deps.RefreshTokenTTL)}
	if err := s.deps.Sessions.CreateSession(ctx, session); err != nil {
		return nil, apperr.NewInternal(err)
	}
	return s.tokenPair(user, refreshToken)
}

func (s *authService) Refresh(ctx context.Context, refreshToken string) (*models.TokenPair, error) {
	hash := auth.HashRefreshToken(refreshToken)
	session, err := s.deps.Sessions.FindSessionByToken(ctx, hash)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, invalidToken(err)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if session.RevokedAt != nil {
		return nil, invalidToken(nil)
	}
	if !bytes.Equal(session.RefreshTokenHash, hash) {
		// The token matched the previous hash: it was already used once.
		return nil, s.revokeReused(ctx, session.ID)
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, invalidToken(nil)
	}

	user, err := s.deps.Users.GetUser(ctx, session.UserID)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, invalidToken(err)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}

	next, nextHash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	session.RefreshTokenHash = nextHash
	session.ExpiresAt = time.Now().Add(s.deps.RefreshTokenTTL)
	err = s.deps.Sessions.RotateRefreshToken(ctx, session, hash)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, s.revokeReused(ctx, session.ID)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return s.tokenPair(user, next)
}

func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	session, err := s.deps.Sessions.FindSessionByToken(ctx, auth.HashRefreshToken(refreshToken))
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	if err := s.deps.Sessions.RevokeSession(ctx, session.ID); err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *authService) LogoutAll(ctx context.Context, userID int64) error {
	if err := s.deps.Users.BumpUserTokenVersion(ctx, userID); err != nil {
		return userError(err)
	}
	if err := s.deps.Sessions.RevokeUserSessions(ctx, userID); err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *authService) Authenticate(ctx context.Context, accessToken string) (auth.Claims, error) {
	claims, err := s.deps.Tokens.Parse(accessToken)
	if err != nil {
		return auth.Claims{}, invalidToken(err)
	}
	user, err := s.deps.Users.GetUser(ctx, claims.UserID)
	if errors.Is(err, apperr.ErrNotFound) {
		return auth.Claims{}, invalidToken(err)
	}
	if err != nil {
		return auth.Claims{}, apperr.NewInternal(err)
	}
	if user.TokenVersion != claims.TokenVersion {
		return auth.Claims{}, invalidToken(errors.New("token version is stale"))
	}
	return claims, nil
}

// revokeReused ends a session whose refresh token was used twice, a sign it was stolen.
func (s *authService) revokeReused(ctx context.Context, sessionID int64) error {
	if err := s.deps.Sessions.RevokeSession(ctx, sessionID); err != nil {
		return apperr.NewInternal(err)
	}
	return invalidToken(errors.New("refresh token reused"))
}

func (s *authService) tokenPair(user *models.User, refreshToken string) (*models.TokenPair, error) {
	access, expiresAt, err := s.deps.Tokens.Issue(user.ID, user.TokenVersion)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return &models.TokenPair{AccessToken: access, AccessTokenExpiresAt: expiresAt, RefreshToken: refreshToken}, nil
}

func invalidCredentials() error {
	return apperr.NewUnauthorized("invalid email or password", nil)
}

func invalidToken(err error) error {
	return apperr.NewUnauthorized("invalid or expired token", err)
}
