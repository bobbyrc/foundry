package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/auth"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store/sqlite"
)

func TestUserResetPasswordPrintsWorkingPassword(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "foundry.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf("server:\n  data_dir: %s\n", dir)), 0o600))

	st, err := sqlite.Open(filepath.Join(dir, "foundry.db"))
	require.NoError(t, err)
	hash, err := auth.HashPassword("the old password!!")
	require.NoError(t, err)
	now := time.Now()
	_, err = st.CreateUser(context.Background(), domain.User{Username: "alice", Role: domain.RoleOwner, PasswordHash: hash, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	require.NoError(t, st.Close())

	var out, errOut bytes.Buffer
	code := run([]string{"user", "reset-password", "alice", "--config", cfgPath}, &out, &errOut)
	require.Equal(t, 0, code, errOut.String())

	m := regexp.MustCompile(`New password for alice: ([A-Za-z0-9]{20})\nSign in and change it\.\n`).FindStringSubmatch(out.String())
	require.NotNil(t, m, "output: %q", out.String())
	password := m[1]

	st, err = sqlite.Open(filepath.Join(dir, "foundry.db"))
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	u, err := st.GetUser(context.Background(), "alice")
	require.NoError(t, err)
	assert.True(t, auth.CheckPassword(u.PasswordHash, password), "the printed password works")
	assert.False(t, auth.CheckPassword(u.PasswordHash, "the old password!!"), "the old one does not")
}

func TestUserResetPasswordUnknownUser(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "foundry.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf("server:\n  data_dir: %s\n", dir)), 0o600))

	var out, errOut bytes.Buffer
	code := run([]string{"user", "reset-password", "ghost", "--config", cfgPath}, &out, &errOut)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut.String(), "no user named ghost")
}
