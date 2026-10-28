package domain

import "time"

// Role is what a person signed in to the web UI may do. The API token used by the CLI and the
// orchestrator is not a user and has no role; see internal/auth for what it may do.
type Role string

const (
	// RoleViewer reads every page and the event stream, and changes nothing.
	RoleViewer Role = "viewer"
	// RoleOperator also acts on work: approves, retries, cancels, pauses, creates tasks, chats.
	RoleOperator Role = "operator"
	// RoleOwner also edits the config file and manages users. There is always at least one.
	RoleOwner Role = "owner"
)

// Valid reports whether r is one of the three roles.
func (r Role) Valid() bool {
	return r == RoleViewer || r == RoleOperator || r == RoleOwner
}

// User is a person who signs in to the web UI. The password hash never leaves the server.
type User struct {
	Username     string    `json:"username"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Session is a signed-in browser. ID is the SHA-256 (hex) of the cookie value, so the database
// never holds a value that could be replayed as a cookie.
type Session struct {
	ID        string    `json:"-"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
