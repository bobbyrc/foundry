package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"

	"github.com/bobbyrc/foundry/internal/auth"
	"github.com/bobbyrc/foundry/internal/store"
	"github.com/bobbyrc/foundry/internal/store/sqlite"
)

// userHelp is the usage text for `foundry user`.
const userHelp = "usage: foundry user reset-password NAME [--config P]\n" +
	"Run \"foundry user <verb> -h\" for a verb's flags."

// resetPasswordLength is the length of the random password `user reset-password` prints.
const resetPasswordLength = 20

// passwordAlphabet is the set the generated password is drawn from: letters and digits.
const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func cmdUser(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: foundry user reset-password NAME")
	}
	sub, rest := args[0], args[1:]
	if isHelpFlag(sub) || sub == "help" {
		fmt.Fprintln(e.out, userHelp)
		return nil
	}
	switch sub {
	case "reset-password":
		return resetUserPassword(e, rest)
	}
	return usagef("unknown user command %q (want reset-password)", sub)
}

// resetUserPassword replaces a user's password with a random one and signs them out everywhere.
// It writes the database directly, so it needs shell access to the machine, the same as serve.
func resetUserPassword(e *env, args []string) error {
	fs := newFlagSet("user reset-password", e)
	cfgFlag := fs.String("config", "", "config file (default $FOUNDRY_CONFIG or ~/.config/foundry/foundry.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "user reset-password NAME [--config P]"); err != nil {
		return err
	}
	name := positional[0]
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if err := loadSecretsFile(filepath.Join(dir, "secrets.env")); err != nil {
		return err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	st, err := sqlite.Open(filepath.Join(cfg.Server.DataDir, "foundry.db"))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	password, err := randomPassword(resetPasswordLength)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.SetUserPassword(context.Background(), name, hash, ""); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no user named %s", name)
		}
		return err
	}
	fmt.Fprintf(e.out, "New password for %s: %s\nSign in and change it.\n", name, password)
	return nil
}

// randomPassword returns n characters drawn uniformly from passwordAlphabet, using crypto/rand.
func randomPassword(n int) (string, error) {
	max := big.NewInt(int64(len(passwordAlphabet)))
	out := make([]byte, n)
	for i := range out {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		out[i] = passwordAlphabet[k.Int64()]
	}
	return string(out), nil
}
