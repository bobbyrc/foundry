// Package notify tells you about things that need your attention.
package notify

import (
	"context"
	"fmt"
	"sync"
)

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high" // blocked work, rollbacks, approvals
)

type Notification struct {
	Title    string
	Body     string
	Priority Priority
	URL      string // deep link into the web UI
	Tags     []string
}

type Notifier interface {
	Notify(ctx context.Context, n Notification) error
}

// Config is one entry of the notify list from onett.yaml.
type Config struct {
	Type        string
	Options     map[string]any
	MinPriority Priority // notifications below this are dropped; default normal
}

type Factory func(cfg Config) (Notifier, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

func New(cfg Config) (Notifier, error) {
	mu.RLock()
	f, ok := factories[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("notify: unknown type %q", cfg.Type)
	}
	return f(cfg)
}
