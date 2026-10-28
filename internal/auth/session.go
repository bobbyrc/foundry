package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// SessionCookie is the name of the browser cookie that carries a session token.
const SessionCookie = "onlet_session"

// SessionTTL is how long a session lasts after sign-in.
const SessionTTL = 30 * 24 * time.Hour

// NewSessionToken makes a new random session. value goes only in the browser's cookie; id is
// what the server stores. Keeping only a hash of the cookie means a leaked database cannot be
// replayed as signed-in cookies.
func NewSessionToken() (value, id string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read session token: %w", err)
	}
	value = base64.RawURLEncoding.EncodeToString(b)
	return value, SessionID(value), nil
}

// SessionID returns the stored identifier for a cookie value: the lowercase hex SHA-256 of it.
func SessionID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
