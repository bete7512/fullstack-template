//go:build integration

package sqs_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/events/sns"
	"github.com/bete7512/scaffold/pkg/events/sqs"
)

type fixture struct {
	pub      *sns.Publisher
	sqs      *awssqs.Client
	queueURL string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	endpoint := os.Getenv("EVENTS_AWS_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("EVENTS_AWS_ENDPOINT_URL not set; start LocalStack with `make queue-up`")
	}
	for k, v := range map[string]string{
		"EVENTS_AWS_REGION":     "us-east-1",
		"AWS_ACCESS_KEY_ID":     "test",
		"AWS_SECRET_ACCESS_KEY": "test",
	} {
		if os.Getenv(k) == "" {
			t.Setenv(k, v)
		}
	}
	region := os.Getenv("EVENTS_AWS_REGION")
	topicARN := os.Getenv("EVENTS_TOPIC_ARN")
	queueURL := os.Getenv("EVENTS_QUEUE_URL")
	require.NotEmpty(t, topicARN, "EVENTS_TOPIC_ARN")
	require.NotEmpty(t, queueURL, "EVENTS_QUEUE_URL")

	ctx := context.Background()
	snsClient, err := sns.NewClient(ctx, region, endpoint)
	require.NoError(t, err)
	sqsClient, err := sqs.NewClient(ctx, region, endpoint)
	require.NoError(t, err)
	f := &fixture{pub: sns.NewPublisher(snsClient, topicARN), sqs: sqsClient, queueURL: queueURL}
	f.purge(t)
	return f
}

// purge empties the queue; SQS allows one purge per minute, so a repeat run drains by hand instead.
func (f *fixture) purge(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := f.sqs.PurgeQueue(ctx, &awssqs.PurgeQueueInput{QueueUrl: aws.String(f.queueURL)})
	var inProgress *types.PurgeQueueInProgress
	if errors.As(err, &inProgress) {
		for {
			out, err := f.sqs.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
				QueueUrl: aws.String(f.queueURL), MaxNumberOfMessages: 10, WaitTimeSeconds: 1,
			})
			require.NoError(t, err)
			if len(out.Messages) == 0 {
				return
			}
			for _, m := range out.Messages {
				_, err := f.sqs.DeleteMessage(ctx, &awssqs.DeleteMessageInput{QueueUrl: aws.String(f.queueURL), ReceiptHandle: m.ReceiptHandle})
				require.NoError(t, err)
			}
		}
	}
	require.NoError(t, err)
}

func mustEvent(t *testing.T, typ string, payload any) events.Event {
	t.Helper()
	e, err := events.New(typ, payload)
	require.NoError(t, err)
	return e
}

func TestPublishSubscribe(t *testing.T) {
	f := newFixture(t)

	tests := []struct {
		name       string
		events     []events.Event
		failFirst  bool
		wantTypes  []string
		visibility time.Duration
	}{
		{
			name: "delivers each published event once",
			events: []events.Event{
				mustEvent(t, "user.registered", map[string]any{"id": 1, "email": "ada@example.com"}),
				mustEvent(t, "user.deleted", map[string]any{"id": 2}),
			},
			wantTypes:  []string{"user.registered", "user.deleted"},
			visibility: 30 * time.Second,
		},
		{
			name:       "redelivers after a handler error",
			events:     []events.Event{mustEvent(t, "user.registered", map[string]any{"id": 3})},
			failFirst:  true,
			wantTypes:  []string{"user.registered", "user.registered"},
			visibility: time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			require.NoError(t, f.pub.Publish(ctx, tt.events))

			delivered := make(chan events.Event, len(tt.wantTypes))
			var calls atomic.Int32
			handler := func(_ context.Context, e events.Event) error {
				delivered <- e
				if tt.failFirst && calls.Add(1) == 1 {
					return errors.New("transient")
				}
				return nil
			}
			sub := sqs.NewSubscriber(f.sqs, f.queueURL, sqs.WithVisibilityTimeout(tt.visibility))
			runErr := make(chan error, 1)
			go func() { runErr <- sub.Run(ctx, handler) }()

			got := make([]events.Event, 0, len(tt.wantTypes))
			for len(got) < len(tt.wantTypes) {
				select {
				case e := <-delivered:
					got = append(got, e)
				case <-ctx.Done():
					require.FailNowf(t, "timed out", "got %d of %d deliveries", len(got), len(tt.wantTypes))
				}
			}
			cancel()
			require.NoError(t, <-runErr)

			gotTypes := make([]string, 0, len(got))
			for _, e := range got {
				gotTypes = append(gotTypes, e.Type)
			}
			assert.ElementsMatch(t, tt.wantTypes, gotTypes)
			byType := map[string]events.Event{}
			for _, e := range tt.events {
				byType[e.Type] = e
			}
			for _, e := range got {
				want := byType[e.Type]
				assert.JSONEq(t, string(want.Payload), string(e.Payload))
				assert.True(t, want.OccurredAt.Equal(e.OccurredAt), "occurred_at round-trips")
			}
		})
	}
}
