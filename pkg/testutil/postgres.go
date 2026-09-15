//go:build integration

package testutil

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/bete7512/scaffold/pkg/db"
)

var (
	once      sync.Once
	pool      *pgxpool.Pool
	container *tcpostgres.PostgresContainer
	setupErr  error
)

// MigrateFunc applies a service's migrations, e.g. migrations.Up.
type MigrateFunc func(ctx context.Context, pool *pgxpool.Pool) error

// Postgres returns a pool shared by the test process, migrated with migrate when it is not nil.
// It uses TEST_DATABASE_URL when set, otherwise it starts a postgres container. Do not close the pool.
func Postgres(t testing.TB, migrate MigrateFunc) *pgxpool.Pool {
	t.Helper()
	once.Do(func() { setup(migrate) })
	if setupErr != nil {
		t.Fatalf("testutil: postgres: %v", setupErr)
	}
	return pool
}

// Main runs the tests and then stops the container. Use it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }
func Main(m *testing.M) int {
	code := m.Run()
	if pool != nil {
		pool.Close()
	}
	if container != nil {
		_ = testcontainers.TerminateContainer(container)
	}
	return code
}

// Truncate empties tables and resets their identity sequences.
func Truncate(t testing.TB, p *pgxpool.Pool, tables ...string) {
	t.Helper()
	stmt := "TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := p.Exec(context.Background(), stmt); err != nil {
		t.Fatalf("testutil: %s: %v", stmt, err)
	}
}

func setup(migrate MigrateFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("scaffold_test"),
			tcpostgres.WithUsername("scaffold"),
			tcpostgres.WithPassword("scaffold"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			setupErr = fmt.Errorf("start container: %w", err)
			return
		}
		container = c
		if url, err = c.ConnectionString(ctx, "sslmode=disable"); err != nil {
			setupErr = fmt.Errorf("connection string: %w", err)
			return
		}
	}

	p, err := db.NewPool(ctx, db.Config{URL: url, MaxConns: 8, MinConns: 1})
	if err != nil {
		setupErr = err
		return
	}
	if migrate == nil {
		pool = p
		return
	}
	if err := migrate(ctx, p); err != nil {
		p.Close()
		setupErr = fmt.Errorf("migrate: %w", err)
		return
	}
	pool = p
}
