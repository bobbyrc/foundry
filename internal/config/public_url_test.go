package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePublicURL(t *testing.T) {
	_, err := Parse([]byte("server:\n  public_url: https://foundry.example.com\n"), "")
	require.NoError(t, err)

	_, err = Parse([]byte("server:\n  public_url: foundry.lan:7777\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `server.public_url "foundry.lan:7777" must be an absolute http or https URL`)
}
