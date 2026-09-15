package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/services/api/internal/config"
)

const (
	dbURL      = "postgres://app:secret@localhost:5432/app"
	signingKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantErrs []string
	}{
		{name: "defaults", env: map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey}},
		{
			name:     "invalid auth settings",
			env:      map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey, "AUTH_ARGON2_MEMORY_KIB": "1024", "AUTH_ARGON2_ITERATIONS": "0"},
			wantErrs: []string{"AUTH_ARGON2_MEMORY_KIB", "AUTH_ARGON2_ITERATIONS"},
		},
		{
			name:     "missing signing key",
			env:      map[string]string{"DATABASE_URL": dbURL},
			wantErrs: []string{"AUTH_JWT_SIGNING_KEY"},
		},
		{
			name:     "shared and service errors together",
			env:      map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey, "APP_ENV": "qa", "AUTH_ARGON2_PARALLELISM": "0"},
			wantErrs: []string{"APP_ENV", "AUTH_ARGON2_PARALLELISM"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Parse(tt.env)
			if len(tt.wantErrs) > 0 {
				require.Error(t, err)
				for _, want := range tt.wantErrs {
					assert.Contains(t, err.Error(), want)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ":8080", cfg.HTTP.Addr)
			assert.Equal(t, uint32(19456), cfg.Auth.Argon2MemoryKiB)
			assert.Len(t, cfg.Auth.SigningKeySeed(), 32)
		})
	}
}
