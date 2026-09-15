//go:build integration

package migrations_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/testutil"
	"github.com/bete7512/scaffold/services/api/migrations"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func TestMigrations(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Postgres(t, migrations.Up)
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, migrations.Up(ctx, pool), "second up is a no-op")
	require.NoError(t, migrations.New(logger).Run(ctx, pool, []string{"status"}), "New twice must not re-register")
	require.Error(t, migrations.New(logger).Run(ctx, pool, nil), "no command")
}
