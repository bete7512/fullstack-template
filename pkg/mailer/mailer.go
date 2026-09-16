// Package mailer defines the outgoing email contract every transport implements.
package mailer

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidMessage is wrapped by every Validate error.
var ErrInvalidMessage = errors.New("invalid message")

// Message is one outgoing email; HTML is optional.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Mailer delivers messages.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// Validate reports a missing recipient, subject or body.
func (m Message) Validate() error {
	switch {
	case m.To == "":
		return fmt.Errorf("%w: to is required", ErrInvalidMessage)
	case m.Subject == "":
		return fmt.Errorf("%w: subject is required", ErrInvalidMessage)
	case m.Text == "" && m.HTML == "":
		return fmt.Errorf("%w: text or html body is required", ErrInvalidMessage)
	}
	return nil
}
