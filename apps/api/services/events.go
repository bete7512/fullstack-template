package services

//go:generate mockgen -source=events.go -destination=service_mocks/mock_events.go -package=service_mocks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/events"
)

// EventService publishes committed events, cleans the outbox and runs each delivered event once.
type EventService interface {
	// Publish publishes every unpublished event, one batch per transaction, and returns how many it published.
	Publish(ctx context.Context) (int, error)
	// CleanOutbox deletes events published longer ago than the retention and returns how many it deleted.
	CleanOutbox(ctx context.Context) (int64, error)
	// ProcessOnce records eventID and runs fn; it reports false without running fn when the event was already processed.
	// An error from fn discards the record, so a redelivery runs fn again.
	ProcessOnce(ctx context.Context, eventID int64, fn func(ctx context.Context) error) (bool, error)
}

// EventServiceDeps are the EventService dependencies; all are required.
type EventServiceDeps struct {
	Tx        db.Transactor
	Outbox    repos.OutboxRepo
	Processed repos.ProcessedEventRepo
	Publisher events.Publisher
	BatchSize int
	Retention time.Duration
	Logger    *slog.Logger
}

type eventService struct {
	deps EventServiceDeps
}

// NewEventService returns an EventService or an error if a dependency is missing.
func NewEventService(deps EventServiceDeps) (EventService, error) {
	if deps.Tx == nil || deps.Outbox == nil || deps.Processed == nil || deps.Publisher == nil || deps.Logger == nil || deps.BatchSize <= 0 || deps.Retention <= 0 {
		return nil, errors.New("services: Tx, Outbox, Processed, Publisher, Logger, BatchSize and Retention are required")
	}
	return &eventService{deps: deps}, nil
}

func (s *eventService) Publish(ctx context.Context) (int, error) {
	var total int
	for {
		n, err := s.publishBatch(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if n < s.deps.BatchSize {
			return total, nil
		}
	}
}

func (s *eventService) publishBatch(ctx context.Context) (int, error) {
	var n int
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		outbox := s.deps.Outbox.WithTx(tx)
		batch, err := outbox.ClaimOutboxEvents(ctx, s.deps.BatchSize)
		if err != nil {
			return err
		}
		n = len(batch)
		if n == 0 {
			return nil
		}
		evs := make([]events.Event, n)
		ids := make([]int64, n)
		for i, e := range batch {
			evs[i] = events.Event{ID: e.ID, Type: e.Type, OccurredAt: e.OccurredAt, Payload: e.Payload}
			ids[i] = e.ID
		}
		if err := s.deps.Publisher.Publish(ctx, evs); err != nil {
			return fmt.Errorf("services: publish events: %w", err)
		}
		return outbox.MarkOutboxEventsPublished(ctx, ids)
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *eventService) CleanOutbox(ctx context.Context) (int64, error) {
	before := time.Now().Add(-s.deps.Retention)
	var total int64
	for {
		n, err := s.deps.Outbox.DeleteOutboxEventsPublishedBefore(ctx, before, s.deps.BatchSize)
		total += n
		if err != nil {
			return total, err
		}
		if n < int64(s.deps.BatchSize) {
			return total, nil
		}
	}
}

func (s *eventService) ProcessOnce(ctx context.Context, eventID int64, fn func(ctx context.Context) error) (bool, error) {
	var fresh bool
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		fresh, err = s.deps.Processed.WithTx(tx).MarkEventProcessed(ctx, eventID)
		if err != nil || !fresh {
			return err
		}
		return fn(ctx)
	})
	if err != nil {
		return false, err
	}
	return fresh, nil
}
