package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetentionDefaults(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	assert.Equal(t, 720*time.Hour, cfg.Server.RetainEvents.Duration)
	assert.Equal(t, 2160*time.Hour, cfg.Server.RetainRunLogs.Duration)
}

func TestRetentionSetValuesAreKept(t *testing.T) {
	cfg, err := Parse([]byte("server:\n  retain_events: 24h\n  retain_run_logs: 0s\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Server.RetainEvents.Duration)
	// An explicit zero is indistinguishable from unset, so it takes the default.
	assert.Equal(t, 2160*time.Hour, cfg.Server.RetainRunLogs.Duration)
}

func TestRetentionMustNotBeNegative(t *testing.T) {
	_, err := Parse([]byte("server:\n  retain_events: -1h\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.retain_events")
	assert.NotContains(t, err.Error(), "server.retain_run_logs")

	_, err = Parse([]byte("server:\n  retain_run_logs: -24h\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.retain_run_logs")
}
