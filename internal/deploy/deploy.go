// Package deploy releases a project and checks that the release is healthy.
package deploy

import (
	"context"
	"fmt"
	"sync"
)

type State string

const (
	StatePending  State = "pending"
	StateBuilding State = "building"
	StateReady    State = "ready"
	StateError    State = "error"
	StateCanceled State = "canceled"
)

type Handle struct {
	ID  string // the provider's deployment ID
	URL string
	// Output is the tail of what the deploy printed (≤ 4KB), kept as evidence for incidents.
	Output string
}

type Status struct {
	State   State
	URL     string
	Message string
}

// Deployer releases one project.
type Deployer interface {
	// Deploy releases ref (a commit SHA). For platforms that deploy on push it finds the
	// deployment for ref instead of creating one.
	Deploy(ctx context.Context, ref string) (Handle, error)
	Status(ctx context.Context, id string) (Status, error)
	// Rollback restores the release identified by id/ref (a previous healthy deployment).
	Rollback(ctx context.Context, id, ref string) error
}

// Config is a project's deploy block from onlet.yaml. Options holds the type-specific keys.
type Config struct {
	Type    string
	Options map[string]any
	// Dir is the project's local clone, the working directory for command deployers.
	Dir string
	Env map[string]string
}

type Factory func(cfg Config) (Deployer, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

func New(cfg Config) (Deployer, error) {
	mu.RLock()
	f, ok := factories[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("deploy: unknown type %q", cfg.Type)
	}
	return f(cfg)
}
