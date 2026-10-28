package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MinPasswordLength is the fewest characters (runes) a password may have.
const MinPasswordLength = 12

const (
	// maxPasswordBytes stops a very long password from costing time in the hash function.
	maxPasswordBytes = 256
	// passwordIterations is the PBKDF2 work factor. It makes each guess cost a real amount of
	// CPU, which is what slows an offline attack on a stolen password file.
	passwordIterations = 600_000
	passwordScheme     = "pbkdf2-sha256"
	saltLength         = 16
	keyLength          = 32
)

// burnSalt is a fixed salt for BurnPasswordCheck. Its value does not matter; it only has to
// be a normal-looking salt so the derivation does the same work as a real check.
var burnSalt = make([]byte, saltLength)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// HashPassword returns the stored form of password: the scheme, the iteration count, a random
// salt and the derived key, joined with "$". The salt is what stops two users with the same
// password from getting the same stored value.
func HashPassword(password string) (string, error) {
	return hashWith(password, passwordIterations)
}

// CheckPassword reports whether password matches hash, a value made by HashPassword. Any hash
// it cannot parse gives false, so a corrupt row can never sign someone in or panic.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != saltLength {
		return false
	}
	stored, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(stored) != keyLength {
		return false
	}
	key, err := derive(password, salt, iterations)
	if err != nil {
		return false
	}
	// Compare in constant time so the response does not reveal how many leading bytes matched.
	return subtle.ConstantTimeCompare(key, stored) == 1
}

// BurnPasswordCheck does one full password derivation and throws the result away. Call it when
// the username does not exist, so signing in as an unknown user takes as long as signing in as
// a real one and the timing does not reveal which usernames exist.
func BurnPasswordCheck(password string) {
	_, _ = derive(password, burnSalt, passwordIterations)
}

// ValidatePassword returns an error saying why password is not acceptable, or nil.
func ValidatePassword(password string) error {
	if len(password) > maxPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", maxPasswordBytes)
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// ValidateUsername returns an error saying why name is not acceptable, or nil. Usernames are
// lowercase so "Alice" and "alice" cannot become two accounts.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("username must be 1 to 64 characters: lowercase letters, digits, dots, " +
			"underscores or hyphens, starting with a letter or digit")
	}
	return nil
}

// hashWith is HashPassword with the iteration count given, so tests can use a small count.
func hashWith(password string, iterations int) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key, err := derive(password, salt, iterations)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s", passwordScheme, iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// derive is the one place the password is run through PBKDF2-SHA256.
func derive(password string, salt []byte, iterations int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, keyLength)
}
