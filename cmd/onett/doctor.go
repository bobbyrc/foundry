package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/harness"
)

// doctor collects one line per check and counts the failures.
type doctor struct {
	out      io.Writer
	failures int
}

// line prints one check: a status (ok, warn or FAIL), its message and, when there is one, the
// fix hint. Only FAIL counts toward the exit code.
func (d *doctor) line(status, msg, fix string) {
	if status == "FAIL" {
		d.failures++
	}
	msg, fix = cleanText(msg), cleanText(fix)
	if fix != "" {
		fmt.Fprintf(d.out, "%-4s  %s — fix: %s\n", status, msg, fix)
		return
	}
	fmt.Fprintf(d.out, "%-4s  %s\n", status, msg)
}

func (d *doctor) ok(msg string)        { d.line("ok", msg, "") }
func (d *doctor) warn(msg, fix string) { d.line("warn", msg, fix) }
func (d *doctor) fail(msg, fix string) { d.line("FAIL", msg, fix) }

// cmdDoctor checks the setup and prints one ok, warn or FAIL line per check. It exits 1 when any
// check fails.
func cmdDoctor(e *env, args []string) error {
	fs := newFlagSet("doctor", e)
	cfgFlag := fs.String("config", "", "config file (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "doctor [--config P]"); err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}

	d := &doctor{out: e.out}
	cfg, cfgErr := loadConfig(path)
	if cfgErr != nil {
		d.fail("config: "+cfgErr.Error(), "fix the file, or run `onett init` to write one")
	} else {
		d.ok("config loads: " + path)
		d.checkDataDir(cfg.Server.DataDir)
	}
	d.checkGit()
	d.checkGitHub()
	if cfg != nil {
		d.checkHarnesses(cfg)
		d.checkProjects(cfg)
	}
	d.checkDaemon(apiBaseURL(cfg))

	if d.failures > 0 {
		return fmt.Errorf("%d check(s) failed", d.failures)
	}
	return nil
}

// checkDataDir confirms the data directory exists (creating it, as serve does) and accepts a file.
func (d *doctor) checkDataDir(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		d.fail(fmt.Sprintf("data dir %s: cannot create: %v", dir, err), "set server.data_dir to a writable directory")
		return
	}
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		d.fail(fmt.Sprintf("data dir %s: not writable: %v", dir, err), "fix the directory's permissions, or set server.data_dir")
		return
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	d.ok("data dir writable: " + dir)
}

func (d *doctor) checkGit() {
	out, err := runExternal("git", "--version")
	if err != nil {
		d.fail("git: "+firstLine(out, err), "install git (xcode-select --install on macOS)")
		return
	}
	d.ok("git: " + firstLine(out, nil))
}

func (d *doctor) checkGitHub() {
	out, err := runExternal("gh", "auth", "status")
	if err != nil {
		d.fail("gh: not authenticated: "+firstLine(out, err), "gh auth login")
		return
	}
	d.ok("gh: authenticated")
}

// checkHarnesses checks that each configured harness can run. A missing CLI is a FAIL when an
// agent uses that harness and a warning when none does.
func (d *doctor) checkHarnesses(cfg *config.Config) {
	names := make([]string, 0, len(cfg.Harness))
	for name := range cfg.Harness {
		names = append(names, name)
	}
	sort.Strings(names)
	known := harness.Types()

	for _, name := range names {
		h := cfg.Harness[name]
		agents := agentsUsing(cfg, name)
		if !containsString(known, h.Type) {
			d.fail(fmt.Sprintf("harness %s: unknown type %q", name, h.Type),
				"use one of: "+strings.Join(known, ", "))
			continue
		}
		var cli string
		var runErr error
		var out string
		switch h.Type {
		case "claude-code":
			cli = "claude"
			out, runErr = runExternal(cli, "--version")
		case "codex":
			cli = "codex"
			out, runErr = runExternal(cli, "--version")
		case "exec":
			if len(h.Command) == 0 {
				d.fail(fmt.Sprintf("harness %s: exec harness has no command", name), "set harnesses."+name+".command")
				continue
			}
			cli = h.Command[0]
			if _, err := exec.LookPath(cli); err != nil {
				runErr = err
			} else {
				out = "found"
			}
		default:
			// The fake harness runs no external program.
			d.ok(fmt.Sprintf("harness %s (type %s): no CLI to check", name, h.Type))
			continue
		}
		if runErr != nil {
			d.missingHarness(name, cli, agents, firstLine(out, runErr))
			continue
		}
		d.ok(fmt.Sprintf("harness %s: %s (%s)", name, cli, firstLine(out, nil)))
	}
}

// missingHarness reports a harness whose CLI cannot run.
func (d *doctor) missingHarness(name, cli string, agents []string, reason string) {
	if len(agents) > 0 {
		d.fail(fmt.Sprintf("harness %s: %s not runnable (%s); agents %s use it", name, cli, reason, strings.Join(agents, ", ")),
			fmt.Sprintf("install %s and make sure it is on PATH", cli))
		return
	}
	d.warn(fmt.Sprintf("harness %s: %s not runnable (%s); no agent uses it", name, cli, reason),
		fmt.Sprintf("install %s if you plan to use this harness", cli))
}

// agentsUsing lists the roles that run on the harness name, sorted. That is every role that
// some context resolves to this harness: the global roles (context ""), and each project's merged
// roles from AgentsFor, so a role only one project has is found too. A role a project re-points
// to name is labelled with the project.
func agentsUsing(cfg *config.Config, name string) []string {
	seen := map[string]bool{}
	for _, ctx := range append([]string{""}, cfg.ProjectNames()...) {
		for role, a := range cfg.AgentsFor(ctx) {
			if a.Harness != name {
				continue
			}
			label := role
			if g, ok := cfg.Agents[role]; ctx != "" && (!ok || g.Harness != name) {
				label = fmt.Sprintf("%s (project %s)", role, ctx)
			}
			seen[label] = true
		}
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// checkProjects checks each project's repository, clone and pause state.
func (d *doctor) checkProjects(cfg *config.Config) {
	for _, name := range cfg.ProjectNames() {
		p := cfg.Projects[name]
		if !githubRepo.MatchString(p.Repo) || strings.HasPrefix(p.Repo, ".") {
			d.warn(fmt.Sprintf("project %s: repo %q is not owner/name; pull requests need a GitHub repository", name, p.Repo),
				"set repo: owner/name for the project in the config")
		} else {
			d.ok(fmt.Sprintf("project %s: repo %s", name, p.Repo))
		}

		switch info, err := os.Stat(p.Path); {
		case err == nil && info.IsDir():
			d.ok(fmt.Sprintf("project %s: clone at %s", name, p.Path))
		case err == nil:
			d.warn(fmt.Sprintf("project %s: %s exists but is not a directory", name, p.Path),
				"move it aside, or set path for the project in the config")
		case os.IsNotExist(err):
			d.ok(fmt.Sprintf("project %s: no clone at %s yet; it will be cloned there", name, p.Path))
		default:
			d.warn(fmt.Sprintf("project %s: cannot read %s: %v", name, p.Path, err), "fix the path's permissions")
		}

		if p.Paused {
			d.warn(fmt.Sprintf("project %s: paused — queued work will not start", name),
				"set paused: false for the project in the config")
		} else {
			d.ok(fmt.Sprintf("project %s: not paused", name))
		}
	}
}

// checkDaemon asks the daemon's health route whether it is up.
func (d *doctor) checkDaemon(base string) {
	ctx, cancel := context.WithTimeout(context.Background(), externalTimeout)
	defer cancel()
	h, err := client.New(base, "").Health(ctx)
	if err != nil {
		d.warn(fmt.Sprintf("daemon: not reachable at %s (%v)", base, err),
			"run `onett serve`, or `onett service install` to keep it running")
		return
	}
	if !h.OK {
		d.warn(fmt.Sprintf("daemon: %s reports not ok", base), "check the daemon log")
		return
	}
	d.ok(fmt.Sprintf("daemon: running at %s (version %s)", base, h.Version))
}

// firstLine returns the first non-empty line of out, or of err when out has none. With a nil err
// and empty out it returns "".
func firstLine(out string, err error) string {
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
