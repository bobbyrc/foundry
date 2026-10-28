package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

// lowIterations keeps tests fast; only TestHashPasswordRoundTrip and TestBurnPasswordCheckRuns
// pay for the real 600,000-iteration work factor.
const lowIterations = 1000

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		t.Fatalf("stored format = %q, want pbkdf2-sha256$600000$<salt>$<key>", hash)
	}
	if !CheckPassword(hash, "correct horse battery") {
		t.Fatal("CheckPassword rejected the password it was hashed from")
	}
}

func TestCheckPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := hashWith("correct horse battery", lowIterations)
	if err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []string{"", "correct horse batterY", "correct horse battery ", "x"} {
		if CheckPassword(hash, wrong) {
			t.Errorf("CheckPassword accepted wrong password %q", wrong)
		}
	}
}

func TestCheckPasswordRejectsMalformedHash(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, saltLength))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, keyLength))
	cases := map[string]string{
		"empty":            "",
		"just text":        "x",
		"wrong prefix":     "pbkdf2-sha1$1000$" + salt + "$" + key,
		"bad salt base64":  "pbkdf2-sha256$1000$!!!$" + key,
		"bad key base64":   "pbkdf2-sha256$1000$" + salt + "$!!!",
		"non-numeric iter": "pbkdf2-sha256$abc$" + salt + "$" + key,
		"zero iterations":  "pbkdf2-sha256$0$" + salt + "$" + key,
		"negative iter":    "pbkdf2-sha256$-5$" + salt + "$" + key,
		"too few fields":   "pbkdf2-sha256$1000$" + salt,
		"short key":        "pbkdf2-sha256$1000$" + salt + "$AAAA",
	}
	for name, hash := range cases {
		t.Run(name, func(t *testing.T) {
			if CheckPassword(hash, "anything at all") {
				t.Errorf("CheckPassword(%q) = true, want false", hash)
			}
		})
	}
}

func TestHashPasswordSaltMakesEveryHashDiffer(t *testing.T) {
	a, err := hashWith("same password here", lowIterations)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashWith("same password here", lowIterations)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of one password are identical; the salt is not random")
	}
	if !CheckPassword(a, "same password here") || !CheckPassword(b, "same password here") {
		t.Fatal("each salted hash should still check against its password")
	}
}

func TestBurnPasswordCheckRuns(t *testing.T) {
	// Nothing to assert about the result; the call must simply finish without panicking.
	BurnPasswordCheck("nobody has this username")
}

func TestValidatePasswordAcceptsAndRejectsEdges(t *testing.T) {
	cases := []struct {
		name     string
		password string
		ok       bool
	}{
		{"11 characters", strings.Repeat("a", 11), false},
		{"12 characters", strings.Repeat("a", 12), true},
		{"empty", "", false},
		{"12 runes of 2 bytes each", strings.Repeat("é", 12), true},
		{"11 runes of 3 bytes each", strings.Repeat("€", 11), false},
		{"256 bytes", strings.Repeat("a", 256), true},
		{"257 bytes", strings.Repeat("a", 257), false},
		{"86 runes of 3 bytes (258 bytes)", strings.Repeat("€", 86), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePassword(c.password)
			if (err == nil) != c.ok {
				t.Errorf("ValidatePassword(%d bytes, %d runes) error = %v, want ok=%v",
					len(c.password), utf8.RuneCountInString(c.password), err, c.ok)
			}
		})
	}
}

func TestValidatePasswordMessageIsPlainSentence(t *testing.T) {
	err := ValidatePassword("short")
	if err == nil || err.Error() != "password must be at least 12 characters" {
		t.Fatalf("error = %v, want %q", err, "password must be at least 12 characters")
	}
}

func TestValidateUsernameAcceptsAndRejectsEdges(t *testing.T) {
	cases := []struct {
		name string
		user string
		ok   bool
	}{
		{"simple", "alice", true},
		{"digits", "42", true},
		{"dots underscores hyphens", "a.b_c-d", true},
		{"64 characters", "a" + strings.Repeat("b", 63), true},
		{"65 characters", "a" + strings.Repeat("b", 64), false},
		{"empty", "", false},
		{"uppercase", "Alice", false},
		{"leading dot", ".alice", false},
		{"leading hyphen", "-alice", false},
		{"leading underscore", "_alice", false},
		{"space", "al ice", false},
		{"at sign", "alice@example", false},
		{"trailing newline", "alice\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateUsername(c.user)
			if (err == nil) != c.ok {
				t.Errorf("ValidateUsername(%q) error = %v, want ok=%v", c.user, err, c.ok)
			}
		})
	}
}
