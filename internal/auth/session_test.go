package auth

import (
	"strings"
	"testing"
)

func TestNewSessionTokenIsUniqueAndIDMatchesSessionID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		value, id, err := NewSessionToken()
		if err != nil {
			t.Fatalf("NewSessionToken: %v", err)
		}
		if seen[value] {
			t.Fatalf("token %q was issued twice", value)
		}
		seen[value] = true
		if id != SessionID(value) {
			t.Fatalf("id = %q, want SessionID(value) = %q", id, SessionID(value))
		}
		if len(id) != 64 {
			t.Fatalf("id has %d characters, want 64", len(id))
		}
		if !isLowerHex(id) {
			t.Fatalf("id %q is not lowercase hex", id)
		}
	}
}

func TestSessionIDIsSHA256Hex(t *testing.T) {
	// SHA-256 of the empty string, a published test vector.
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := SessionID(""); got != want {
		t.Fatalf("SessionID(\"\") = %q, want %q", got, want)
	}
}

func isLowerHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
