// Package testutil holds test-only helpers shared across packages.
//
// This file has no build tag so unit tests can import the tiny helpers. The
// Postgres helpers live in postgres.go behind `//go:build integration` so a
// plain `go test ./...` never links testcontainers or touches Docker.
package testutil

import (
	"os"
	"testing"
)

// Ptr returns a pointer to v — for populating *string / *time.Time fields
// in table-driven tests without a temporary variable.
func Ptr[T any](v T) *T { return &v }

// MustEnv returns the environment variable key or fails the test.
func MustEnv(t testing.TB, key string) string {
	t.Helper()
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		t.Fatalf("testutil: required env %s is not set", key)
	}
	return v
}
