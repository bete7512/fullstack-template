// Package config is the api service's configuration: the shared settings plus its own.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"

	base "github.com/bete7512/scaffold/pkg/config"
)

// Config is the shared configuration plus the api service's settings.
type Config struct {
	base.Config
	Auth Auth `envPrefix:"AUTH_"`
}

// Auth configures password hashing.
type Auth struct {
	Argon2MemoryKiB   uint32 `env:"ARGON2_MEMORY_KIB" envDefault:"19456"`
	Argon2Iterations  uint32 `env:"ARGON2_ITERATIONS" envDefault:"2"`
	Argon2Parallelism uint8  `env:"ARGON2_PARALLELISM" envDefault:"1"`

	JWTSigningKey   string        `env:"JWT_SIGNING_KEY"`
	Issuer          string        `env:"ISSUER"`
	AccessTokenTTL  time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"720h"`
}

// SigningKeySeed returns the decoded ed25519 seed for signing access tokens.
func (a Auth) SigningKeySeed() []byte {
	seed, _ := base64.StdEncoding.DecodeString(a.JWTSigningKey)
	return seed
}

// Load reads and validates the process environment.
func Load() (Config, error) {
	return parse(env.Options{})
}

// Parse reads and validates the given environment instead of the process one.
func Parse(environment map[string]string) (Config, error) {
	return parse(env.Options{Environment: environment})
}

func parse(opts env.Options) (Config, error) {
	var cfg Config
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate returns every invalid setting at once.
func (c Config) Validate() error {
	var errs []error
	if err := c.Config.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.Auth.Argon2MemoryKiB < 8*1024 {
		errs = append(errs, errors.New("AUTH_ARGON2_MEMORY_KIB must be at least 8192"))
	}
	if c.Auth.Argon2Iterations == 0 {
		errs = append(errs, errors.New("AUTH_ARGON2_ITERATIONS must be positive"))
	}
	if c.Auth.Argon2Parallelism == 0 {
		errs = append(errs, errors.New("AUTH_ARGON2_PARALLELISM must be positive"))
	}
	if len(c.Auth.SigningKeySeed()) != 32 {
		errs = append(errs, errors.New("AUTH_JWT_SIGNING_KEY must be base64 of 32 random bytes (openssl rand -base64 32)"))
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= 0 {
		errs = append(errs, errors.New("AUTH_ACCESS_TOKEN_TTL and AUTH_REFRESH_TOKEN_TTL must be positive"))
	}
	return errors.Join(errs...)
}
