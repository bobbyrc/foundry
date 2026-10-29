package auth

import "github.com/bobbyrc/onett/internal/domain"

// Action is something a request asks the server to do, grouped by how much trust it needs.
type Action int

const (
	// Read covers every GET: looking at tasks, runs, events and config.
	Read Action = iota
	// Operate covers routes that change work: approve, retry, cancel, pause, create a task,
	// chat, run a loop, reload the config.
	Operate
	// Administer covers routes that change the setup itself: edit the config and manage users.
	Administer
)

// Allows reports whether a signed-in user with role may perform a. An invalid role allows
// nothing, so a corrupt role value fails closed.
func Allows(role domain.Role, a Action) bool {
	switch role {
	case domain.RoleViewer:
		return a == Read
	case domain.RoleOperator:
		return a == Read || a == Operate
	case domain.RoleOwner:
		return a >= Read && a <= Administer
	}
	return false
}

// TokenAllows reports whether the API token may perform a. The token is held by the CLI and by
// the orchestrator agent, and agents can read whatever they hold, so it is limited to Read and
// Operate. It must never allow Administer: config and users must not be changeable by an agent
// that happens to have the token.
func TokenAllows(a Action) bool {
	return a == Read || a == Operate
}
