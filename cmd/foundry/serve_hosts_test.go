package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/foundry/internal/config"
)

func TestServeAllowedHosts(t *testing.T) {
	got := serveAllowedHosts(config.Server{
		Addr:         "192.168.1.10:7777",
		PublicURL:    "https://foundry.example.com/ui",
		AllowedHosts: []string{"foundry.lan:7777"},
	})
	assert.Equal(t, []string{"192.168.1.10", "foundry.example.com", "foundry.lan:7777"}, got)

	// A wildcard bind address is not a name clients use, so only the list is allowed.
	got = serveAllowedHosts(config.Server{
		Addr:         "0.0.0.0:9000",
		PublicURL:    "http://0.0.0.0:9000",
		AllowedHosts: []string{"foundry.lan"},
	})
	assert.Equal(t, []string{"foundry.lan"}, got)

	got = serveAllowedHosts(config.Server{Addr: "[::]:7777", PublicURL: "http://[::]:7777"})
	assert.Empty(t, got)
}
