package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0002_AddUsersCreatedAtIndex() {
	goose.AddMigrationNoTxContext(s.up_0002_AddUsersCreatedAtIndex, s.down_0002_AddUsersCreatedAtIndex)
}

func (s *service) up_0002_AddUsersCreatedAtIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS users_created_at_id_idx ON users (created_at DESC, id DESC)`)
	return err
}

func (s *service) down_0002_AddUsersCreatedAtIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS users_created_at_id_idx`)
	return err
}
