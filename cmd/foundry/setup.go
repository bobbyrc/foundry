package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
)

// projectName matches names usable as YAML keys and in CLI flags.
var projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// selfRepo is foundry's own repository; a project for it becomes self_project.
const selfRepo = "bobbyrc/foundry"

func cmdInit(e *env, args []string) error {
	fs := newFlagSet("init", e)
	cfgFlag := fs.String("config", "", "config file to write (default $FOUNDRY_CONFIG or ~/.config/foundry/foundry.yaml)")
	project := fs.String("project", "", "add this project to the config")
	repo := fs.String("repo", "", "the project's GitHub repository, owner/name (with --project)")
	dir := fs.String("path", "", "the project's local clone (with --project; default <data_dir>/repos/<name>)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "init [--config P] [--project NAME --repo OWNER/NAME [--path DIR]]"); err != nil {
		return err
	}
	if (*project == "") != (*repo == "") {
		return usagef("--project and --repo go together")
	}
	if *project == "" && *dir != "" {
		return usagef("--path needs --project")
	}
	if *project != "" && !projectName.MatchString(*project) {
		return usagef("project name %q may use letters, digits, '.', '_' and '-'", *project)
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite it", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	_, werr := f.WriteString(initialConfig(*project, *repo, *dir))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write %s: %w", path, werr)
	}

	serveHint := "foundry serve"
	if *cfgFlag != "" {
		serveHint += " --config " + *cfgFlag
	}
	fmt.Fprintf(e.out, "Wrote %s\n\nNext steps:\n", path)
	fmt.Fprintf(e.out, "  %s    start the daemon\n", serveHint)
	fmt.Fprintln(e.out, "  foundry open     open the web UI in a browser")
	fmt.Fprintln(e.out, "\nfoundry.example.yaml in the foundry repository lists every option.")
	return nil
}

// initialConfig is the starter foundry.yaml. Every key it leaves out has a default.
func initialConfig(project, repo, dir string) string {
	var b strings.Builder
	b.WriteString("# foundry.yaml: the daemon's settings. Every key is optional; unset keys take the\n")
	b.WriteString("# defaults. foundry.example.yaml in the foundry repository lists every option.\n\n")
	b.WriteString("server:\n")
	b.WriteString("  addr: 127.0.0.1:7777      # where the API and web UI listen\n")
	b.WriteString("  # data_dir defaults to ~/.local/share/foundry (database, token, clones, worktrees)\n\n")
	b.WriteString("notify:\n")
	b.WriteString("  - type: log               # write notifications to the daemon log\n")
	if project != "" {
		b.WriteString("\n")
		if repo == selfRepo {
			fmt.Fprintf(&b, "self_project: %s           # foundry itself: friction is filed here\n\n", project)
		}
		b.WriteString("projects:\n")
		fmt.Fprintf(&b, "  %s:\n", project)
		fmt.Fprintf(&b, "    repo: %s\n", strconv.Quote(repo))
		if dir != "" {
			fmt.Fprintf(&b, "    path: %s\n", strconv.Quote(dir))
		}
	}
	return b.String()
}

func cmdToken(e *env, args []string) error {
	fs := newFlagSet("token", e)
	cfgFlag := fs.String("config", "", "config file (default $FOUNDRY_CONFIG or ~/.config/foundry/foundry.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "token [--config P]"); err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	cfg, cfgErr := loadConfig(path)
	if cfgErr != nil && os.Getenv("FOUNDRY_TOKEN") == "" {
		return cfgErr
	}
	token, err := apiToken(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.out, token)
	return nil
}

func cmdOpen(e *env, args []string) error {
	fs := newFlagSet("open", e)
	cfgFlag := fs.String("config", "", "config file (default $FOUNDRY_CONFIG or ~/.config/foundry/foundry.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "open [--config P]"); err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	// The browser signs in with a password, so no token goes in the URL. The config is only
	// needed for the address, which FOUNDRY_API can give instead.
	cfg, cfgErr := loadConfig(path)
	if cfgErr != nil && os.Getenv("FOUNDRY_API") == "" {
		return cfgErr
	}
	url := strings.TrimRight(publicBaseURL(cfg), "/") + "/"

	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, url).Run(); err != nil {
		fmt.Fprintf(e.out, "Could not open a browser (%v). Open this URL:\n%s\n", err, url)
	}
	return nil
}

// publicBaseURL is the address the web UI is reached at: $FOUNDRY_API, then the config's
// public_url, then the default address.
func publicBaseURL(cfg *config.Config) string {
	if v := os.Getenv("FOUNDRY_API"); v != "" {
		return v
	}
	if cfg != nil && cfg.Server.PublicURL != "" {
		return cfg.Server.PublicURL
	}
	return apiBaseURL(cfg)
}
