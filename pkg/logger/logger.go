// Package logger builds the slog logger and adds the request id from context to every record.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Options configures New.
type Options struct {
	Level   string // debug | info | warn | error; defaults to info
	Format  string // json | text; defaults to json
	Service string
	Version string
	Env     string
	Writer  io.Writer // defaults to os.Stdout
}

// New returns a logger that tags every record with service, version, env and the request id.
func New(o Options) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(o.Level)); err != nil {
		level = slog.LevelInfo
	}
	w := o.Writer
	if w == nil {
		w = os.Stdout
	}
	opts := &slog.HandlerOptions{Level: level, AddSource: level <= slog.LevelDebug, ReplaceAttr: utcTimestamp}

	var h slog.Handler = slog.NewJSONHandler(w, opts)
	if o.Format == "text" {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(ctxHandler{h}).With("service", o.Service, "version", o.Version, "env", o.Env)
}

type requestIDKey struct{}

// WithRequestID stores the request id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom returns the request id stored in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type ctxHandler struct {
	slog.Handler
}

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFrom(ctx); id != "" {
		r = r.Clone()
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h ctxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return ctxHandler{h.Handler.WithAttrs(attrs)}
}

func (h ctxHandler) WithGroup(name string) slog.Handler {
	return ctxHandler{h.Handler.WithGroup(name)}
}

func utcTimestamp(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		a.Key = "ts"
		a.Value = slog.TimeValue(a.Value.Time().UTC())
	}
	return a
}
