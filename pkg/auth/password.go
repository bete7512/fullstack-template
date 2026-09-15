// Package auth provides credential primitives shared by services.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ErrInvalidHash means a stored password hash could not be parsed.
var ErrInvalidHash = errors.New("auth: invalid password hash")

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encoded string) (bool, error)
}

// Argon2idParams tunes argon2id. Memory is in KiB.
type Argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2idParams returns the OWASP 2023 recommendation: 19 MiB, 2 iterations, 1 lane.
func DefaultArgon2idParams() Argon2idParams {
	return Argon2idParams{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

type argon2id struct {
	p Argon2idParams
}

// NewArgon2id returns a PasswordHasher using argon2id.
func NewArgon2id(p Argon2idParams) PasswordHasher {
	return &argon2id{p: p}
}

var b64 = base64.RawStdEncoding

// Hash returns a PHC string: $argon2id$v=19$m=<KiB>,t=<iterations>,p=<lanes>$<salt>$<key>.
func (h *argon2id) Hash(password string) (string, error) {
	salt := make([]byte, h.p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.p.Iterations, h.p.Memory, h.p.Parallelism, h.p.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.p.Memory, h.p.Iterations, h.p.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify checks password against encoded using the parameters stored in encoded,
// so hashes made with older parameters still verify.
func (h *argon2id) Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil ||
		memory == 0 || iterations == 0 || parallelism == 0 {
		return false, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > 1024 {
		return false, ErrInvalidHash
	}

	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(key))) //nolint:gosec // len(key) is at most 1024
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}
