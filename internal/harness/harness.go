// Package harness runs coding-agent CLIs non-interactively. Each adapter (claude, codex, exec,
// fake) turns one CLI's output into a common stream of Events.
package harness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// Spec is everything needed to start one agent session.
type Spec struct {
	RunID   string
	Workdir string
	// Prompt is the user turn: the task itself.
	Prompt string
	// Instructions are appended to the agent's system prompt: role, rules, outbox contract.
	Instructions string
	Model        string
	// ResumeSessionID continues an earlier session (chat turns, re-runs on the same branch).
	ResumeSessionID string
	// Env is added to the inherited environment.
	Env map[string]string
	// Args are extra adapter-specific CLI arguments from the agent profile.
	Args []string
}

// Event is one item of a session's output stream.
type Event struct {
	Time time.Time
	Kind domain.LogKind
	Text string
	// ToolID pairs a tool_use event with its tool_result event.
	ToolID string
	// SessionID is set on the first event that reveals it.
	SessionID string
}

// Result is how a session ended.
type Result struct {
	SessionID string
	ExitCode  int
	// FinalText is the agent's last assistant message.
	FinalText   string
	Denials     []string // human-readable description of each refused tool call
	CostUSD     float64
	InputTokens int64
	// CachedInputTokens is the part of InputTokens served from cache (0 if not reported).
	CachedInputTokens int64
	OutputTokens      int64
	IsError           bool
}

// Session is a running agent process.
type Session interface {
	// Events is closed when the process has exited and all output is delivered.
	Events() <-chan Event
	// Wait blocks until the process exits and returns the result. Safe to call once Events is
	// drained or concurrently with draining it.
	Wait() (Result, error)
	// Cancel terminates the process (SIGTERM, then SIGKILL after a grace period).
	Cancel()
}

// ErrInputClosed is returned by Steerable.Send once the session has stopped taking input.
var ErrInputClosed = errors.New("harness: session input is closed")

// Steerable is a Session that accepts more user input while it runs.
type Steerable interface {
	// Send writes one user message to the running agent. It returns ErrInputClosed once the
	// session has stopped taking input; the caller then queues the message instead.
	Send(text string) error
}

// Harness starts sessions of one agent CLI.
type Harness interface {
	Start(ctx context.Context, spec Spec) (Session, error)
}

// Factory builds a harness from its config block.
type Factory func(cfg Config) (Harness, error)

// Config is the harness block from onett.yaml.
type Config struct {
	Name    string
	Type    string            // registered factory name
	Command []string          // binary and fixed leading args; adapters have a default
	Env     map[string]string // added to every session
	Args    []string          // added to every session, before Spec.Args
	// InheritEnv names variables passed through from onett's environment on top of the
	// base allowlist (see proc.go). Everything else in onett's environment is withheld.
	InheritEnv []string
}

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes a harness type available to config. Adapters call it from init.
func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

// New builds a harness of cfg.Type.
func New(cfg Config) (Harness, error) {
	mu.RLock()
	f, ok := factories[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("harness %q: unknown type %q (known: %v)", cfg.Name, cfg.Type, Types())
	}
	return f(cfg)
}

// Types lists registered harness types.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for t := range factories {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
