package db

import (
	"errors"
	"time"
)

// Config configures the pgx pool.
type Config struct {
	URL               string        `env:"DATABASE_URL"`
	MaxConns          int32         `env:"DB_MAX_CONNS" envDefault:"10"`
	MinConns          int32         `env:"DB_MIN_CONNS" envDefault:"2"`
	MaxConnLifetime   time.Duration `env:"DB_MAX_CONN_LIFETIME" envDefault:"30m"`
	MaxConnIdleTime   time.Duration `env:"DB_MAX_CONN_IDLE_TIME" envDefault:"5m"`
	HealthCheckPeriod time.Duration `env:"DB_HEALTHCHECK_PERIOD" envDefault:"30s"`
	ConnectTimeout    time.Duration `env:"DB_CONNECT_TIMEOUT"`
}

// Validate reports the first invalid field; zero durations use pgx defaults.
func (c Config) Validate() error {
	switch {
	case c.URL == "":
		return errors.New("DATABASE_URL is required")
	case c.MaxConns <= 0:
		return errors.New("DB_MAX_CONNS must be positive")
	case c.MinConns < 0 || c.MinConns > c.MaxConns:
		return errors.New("DB_MIN_CONNS must be between 0 and DB_MAX_CONNS")
	}
	return nil
}
