// Package events names the api service's event types and their payloads.
package events

// UserRegistered is published when a user registers.
const UserRegistered = "user.registered"

// UserRegisteredPayload identifies the new user; consumers load the rest.
type UserRegisteredPayload struct {
	UserID int64 `json:"user_id"`
}
