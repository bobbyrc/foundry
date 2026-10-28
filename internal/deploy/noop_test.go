package deploy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopDeployer(t *testing.T) {
	d, err := New(Config{Type: "noop"})
	require.NoError(t, err)

	h, err := d.Deploy(context.Background(), "abc")
	require.NoError(t, err)
	assert.Equal(t, "noop-abc", h.ID)
	assert.Empty(t, h.Output, "noop runs nothing, so it has no deploy output")

	st, err := d.Status(context.Background(), h.ID)
	require.NoError(t, err)
	assert.Equal(t, StateReady, st.State)

	assert.NoError(t, d.Rollback(context.Background(), h.ID, "prev"))
}
