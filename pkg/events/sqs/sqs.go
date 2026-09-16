// Package sqs delivers events from an Amazon SQS queue to a handler.
package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/bete7512/scaffold/pkg/events"
)

const (
	receiveBatchSize  = 10
	receiveWait       = 20 * time.Second
	receiveRetryDelay = time.Second
	deleteTimeout     = 5 * time.Second
	defaultVisibility = 30 * time.Second
)

// NewClient builds an SQS client for region from the default credential chain; endpoint overrides the AWS URL (LocalStack).
func NewClient(ctx context.Context, region, endpoint string) (*awssqs.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("sqs: load aws config: %w", err)
	}
	return awssqs.NewFromConfig(cfg, func(o *awssqs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

// Subscriber delivers messages of one queue to a Handler.
type Subscriber struct {
	client     *awssqs.Client
	queueURL   string
	logger     *slog.Logger
	visibility time.Duration
}

// Option configures a Subscriber.
type Option func(*Subscriber)

// WithLogger sets the logger used for receive, decode and handler failures.
func WithLogger(l *slog.Logger) Option {
	return func(s *Subscriber) { s.logger = l }
}

// WithVisibilityTimeout sets how long a received message stays hidden before redelivery.
func WithVisibilityTimeout(d time.Duration) Option {
	return func(s *Subscriber) { s.visibility = d }
}

// NewSubscriber returns a Subscriber for queueURL.
func NewSubscriber(client *awssqs.Client, queueURL string, opts ...Option) *Subscriber {
	s := &Subscriber{client: client, queueURL: queueURL, logger: slog.Default(), visibility: defaultVisibility}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run long-polls the queue until ctx ends; a nil handler result deletes the message, an error leaves it for redelivery.
func (s *Subscriber) Run(ctx context.Context, h events.Handler) error {
	for ctx.Err() == nil {
		out, err := s.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
			QueueUrl:              aws.String(s.queueURL),
			MaxNumberOfMessages:   receiveBatchSize,
			WaitTimeSeconds:       int32(receiveWait.Seconds()),
			VisibilityTimeout:     int32(s.visibility.Seconds()),
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil //nolint:nilerr // cancellation is the normal way Run stops
			}
			s.logger.WarnContext(ctx, "sqs receive failed", "error", err)
			sleep(ctx, receiveRetryDelay)
			continue
		}
		for _, m := range out.Messages {
			s.handle(ctx, h, m)
		}
	}
	return nil
}

func (s *Subscriber) handle(ctx context.Context, h events.Handler, m types.Message) {
	var e events.Event
	if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &e); err != nil {
		s.logger.WarnContext(ctx, "dropping undecodable message", "message_id", aws.ToString(m.MessageId), "error", err)
		s.delete(ctx, m)
		return
	}
	if err := h(ctx, e); err != nil {
		s.logger.WarnContext(ctx, "event handler failed, left for redelivery",
			"message_id", aws.ToString(m.MessageId), "event_id", e.ID, "event_type", e.Type, "error", err)
		return
	}
	s.delete(ctx, m)
}

// delete acks m even when ctx was cancelled mid-batch, so a handled event is not redelivered on shutdown.
func (s *Subscriber) delete(ctx context.Context, m types.Message) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteTimeout)
	defer cancel()
	_, err := s.client.DeleteMessage(dctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.queueURL),
		ReceiptHandle: m.ReceiptHandle,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "sqs delete failed", "message_id", aws.ToString(m.MessageId), "error", err)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
