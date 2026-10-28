// Package auth holds the security primitives for signing people in to the web UI: password
// hashing, session tokens, a limit on repeated failed sign-ins, and which role may do what.
// It uses only the Go standard library, so the parts that decide who gets in stay small and
// easy to audit.
package auth
