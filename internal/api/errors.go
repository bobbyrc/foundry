package api

import "errors"

var (
	// ErrInvalid marks a request the caller must fix (HTTP 400).
	ErrInvalid = errors.New("invalid request")
	// ErrConflict marks a request that clashes with current state (HTTP 409).
	ErrConflict = errors.New("conflict")
)
