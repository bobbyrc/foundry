// Package observe reads production signals (errors, alerts) for a project.
package observe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
)

// Observer reports production problems seen since a point in time.
type Observer interface {
	Poll(ctx context.Context, since time.Time) ([]domain.SignalObservation, error)
}

// Config is one entry of a project's observe list from onlet.yaml.
type Config struct {
	Name    string // unique per project; becomes Signal.Source
	Type    string
	Options map[string]any
	Env     map[string]string
}

type Factory func(cfg Config) (Observer, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

func New(cfg Config) (Observer, error) {
	mu.RLock()
	f, ok := factories[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("observer %q: unknown type %q", cfg.Name, cfg.Type)
	}
	return f(cfg)
}
