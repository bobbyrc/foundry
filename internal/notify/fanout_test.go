package notify

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const recorderType = "fanout-test-recorder"

// recorder is a test notifier that remembers what it was sent and can be told to fail.
type recorder struct {
	name string
	err  error

	mu   sync.Mutex
	seen []Notification
}

func (r *recorder) Notify(_ context.Context, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, n)
	return r.err
}

// recorders maps a test notifier name to its instance. The factory looks names up here.
var (
	recordersMu sync.Mutex
	recorders   = map[string]*recorder{}
)

func init() {
	Register(recorderType, func(cfg Config) (Notifier, error) {
		name := fmt.Sprint(cfg.Options["name"])
		recordersMu.Lock()
		defer recordersMu.Unlock()
		r, ok := recorders[name]
		if !ok {
			return nil, fmt.Errorf("no recorder named %q", name)
		}
		return r, nil
	})
}

// useRecorder registers a recorder under name for the duration of the test and returns it.
func useRecorder(t *testing.T, name string, err error) *recorder {
	t.Helper()
	r := &recorder{name: name, err: err}
	recordersMu.Lock()
	recorders[name] = r
	recordersMu.Unlock()
	t.Cleanup(func() {
		recordersMu.Lock()
		delete(recorders, name)
		recordersMu.Unlock()
	})
	return r
}

func recorderConfig(name string, min Priority) Config {
	return Config{Type: recorderType, Options: map[string]any{"name": name}, MinPriority: min}
}

func TestFanoutPriorityFiltering(t *testing.T) {
	tests := []struct {
		name     string
		priority Priority
		want     []string // recorder names that should receive the notification
	}{
		{name: "low reaches only the low-threshold notifier", priority: PriorityLow, want: []string{"low"}},
		{name: "normal reaches low and normal", priority: PriorityNormal, want: []string{"low", "normal"}},
		{name: "high reaches all three", priority: PriorityHigh, want: []string{"low", "normal", "high"}},
		{name: "empty counts as normal", priority: "", want: []string{"low", "normal"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			low := useRecorder(t, "low", nil)
			normal := useRecorder(t, "normal", nil)
			high := useRecorder(t, "high", nil)
			f, err := NewFanout([]Config{
				recorderConfig("low", PriorityLow),
				recorderConfig("normal", ""),
				recorderConfig("high", PriorityHigh),
			}, nil)
			require.NoError(t, err)

			n := Notification{Title: "t", Priority: tt.priority}
			require.NoError(t, f.Notify(context.Background(), n))

			got := map[string]*recorder{"low": low, "normal": normal, "high": high}
			for name, r := range got {
				wantHit := false
				for _, w := range tt.want {
					if w == name {
						wantHit = true
					}
				}
				r.mu.Lock()
				if wantHit {
					assert.Len(t, r.seen, 1, "notifier %s", name)
				} else {
					assert.Empty(t, r.seen, "notifier %s", name)
				}
				r.mu.Unlock()
			}
		})
	}
}

func TestFanoutJoinsErrors(t *testing.T) {
	errA := errors.New("boom a")
	errB := errors.New("boom b")
	useRecorder(t, "fail-a", errA)
	useRecorder(t, "ok", nil)
	useRecorder(t, "fail-b", errB)

	f, err := NewFanout([]Config{
		recorderConfig("fail-a", ""),
		recorderConfig("ok", ""),
		recorderConfig("fail-b", ""),
	}, nil)
	require.NoError(t, err)

	err = f.Notify(context.Background(), Notification{Title: "t", Priority: PriorityHigh})
	require.Error(t, err)
	assert.ErrorIs(t, err, errA)
	assert.ErrorIs(t, err, errB)
	assert.Contains(t, err.Error(), recorderType)
}

func TestFanoutNoErrorWhenAllSucceed(t *testing.T) {
	useRecorder(t, "ok1", nil)
	useRecorder(t, "ok2", nil)
	f, err := NewFanout([]Config{recorderConfig("ok1", ""), recorderConfig("ok2", "")}, nil)
	require.NoError(t, err)

	assert.NoError(t, f.Notify(context.Background(), Notification{Title: "t"}))
}

func TestFanoutEmptyConfigIsNoop(t *testing.T) {
	f, err := NewFanout(nil, nil)
	require.NoError(t, err)
	assert.NoError(t, f.Notify(context.Background(), Notification{Title: "t", Priority: PriorityHigh}))
}

func TestNewFanoutValidation(t *testing.T) {
	useRecorder(t, "v", nil)

	_, err := NewFanout([]Config{{Type: "no-such-type"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-type")

	_, err = NewFanout([]Config{recorderConfig("v", "urgent")}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_priority")

	_, err = NewFanout([]Config{recorderConfig("missing", "")}, nil)
	require.Error(t, err)
}
