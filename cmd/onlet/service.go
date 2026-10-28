package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/bobbyrc/onlet/internal/config"
)

// serviceLabel is the launchd label of the daemon. The plist file is named after it.
const serviceLabel = "io.onlet.serve"

// logFileName is the daemon's log, written under the config's data directory.
const logFileName = "onlet.log"

// externalTimeout bounds each command the service and doctor commands run.
const externalTimeout = 10 * time.Second

// Package variables so tests can stand in for the host: the OS, the user id, the LaunchAgents
// directory and the command runner. Production code never reassigns them.
var (
	hostOS = runtime.GOOS

	currentUID = os.Getuid

	// launchAgentsDir is ~/Library/LaunchAgents, where launchd finds per-user agents.
	launchAgentsDir = func() string {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "Library", "LaunchAgents")
	}()

	// commandRunner runs an external program and returns its combined stdout and stderr.
	commandRunner = func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return string(out), err
	}
)

// runExternal runs one external command through commandRunner with externalTimeout.
func runExternal(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), externalTimeout)
	defer cancel()
	return commandRunner(ctx, name, args...)
}

// cmdService installs, removes or reports the launchd agent that keeps `onlet serve` running.
func cmdService(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: onlet service install|uninstall|status [--config P]")
	}
	action := args[0]
	switch action {
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(e.out, "usage: onlet service install|uninstall|status [--config P]")
		return nil
	case "install", "uninstall", "status":
	default:
		return usagef("unknown service action %q: want install, uninstall or status", action)
	}
	fs := newFlagSet("service "+action, e)
	cfgFlag := fs.String("config", "", "config file (default $ONLET_CONFIG or ~/.config/onlet/onlet.yaml)")
	positional, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "service "+action+" [--config P]"); err != nil {
		return err
	}
	if hostOS != "darwin" {
		return fmt.Errorf("onlet service uses launchd, which only runs on macOS (this is %s)", hostOS)
	}
	switch action {
	case "install":
		return serviceInstall(e, *cfgFlag)
	case "uninstall":
		return serviceUninstall(e)
	default:
		return serviceStatus(e, *cfgFlag)
	}
}

// serviceInstall writes the plist and loads it. When the plist already exists it only restarts
// the running service, so repeated installs are harmless.
func serviceInstall(e *env, cfgFlag string) error {
	plist, err := plistPath()
	if err != nil {
		return err
	}
	target := serviceTarget()
	if _, err := os.Stat(plist); err == nil {
		fmt.Fprintf(e.out, "already installed: %s\n", plist)
		if _, err := runLaunchctl("kickstart", "-k", target); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "restarted %s\n", serviceLabel)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	cfgPath, err := configPath(cfgFlag)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	absCfg, err := filepath.Abs(cfgPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	exe, err := onletExecutable()
	if err != nil {
		return err
	}
	dataDir, err := filepath.Abs(cfg.Server.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	// launchd opens the log files before the daemon starts, so the directory must exist.
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	logPath := filepath.Join(dataDir, logFileName)
	// launchd starts the daemon with none of this shell's environment, so record each variable
	// the config references: without them the config does not load. PATH is recorded anyway.
	data, err := os.ReadFile(absCfg)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var vars []envVar
	for _, name := range config.EnvRefs(data) {
		if name != "PATH" {
			vars = append(vars, envVar{Name: name, Value: os.Getenv(name)})
		}
	}

	body, err := renderPlist(plistSpec{
		Args:    []string{exe, "serve", "--config", absCfg},
		Path:    os.Getenv("PATH"),
		Env:     vars,
		LogPath: logPath,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(plist), err)
	}
	// Only the user may read it: recorded variables can hold secrets.
	if err := os.WriteFile(plist, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", plist, err)
	}
	if _, err := runLaunchctl("bootstrap", launchdDomain(), plist); err != nil {
		// Leave no half-installed plist behind: the next install should start clean.
		_ = os.Remove(plist)
		return err
	}
	fmt.Fprintf(e.out, "installed %s\n  plist: %s\n  log:   %s\n", serviceLabel, plist, logPath)
	if len(vars) > 0 {
		names := make([]string, len(vars))
		for i, v := range vars {
			names[i] = v.Name
		}
		fmt.Fprintf(e.out, "  env:   %s (from the config; uninstall and install again to change them)\n", strings.Join(names, ", "))
	}
	return nil
}

// serviceUninstall unloads the service and removes its plist. A service that was not loaded is
// not an error.
func serviceUninstall(e *env) error {
	plist, err := plistPath()
	if err != nil {
		return err
	}
	out, err := runExternal("launchctl", "bootout", serviceTarget())
	if err != nil && !notLoadedOutput(out) {
		return launchctlError([]string{"bootout", serviceTarget()}, out, err)
	}
	if err := os.Remove(plist); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(e.out, "%s is not installed\n", serviceLabel)
			return nil
		}
		return fmt.Errorf("remove %s: %w", plist, err)
	}
	fmt.Fprintf(e.out, "uninstalled %s (removed %s)\n", serviceLabel, plist)
	return nil
}

// pidLine matches the pid line of `launchctl print`, which is present only while the process runs.
var pidLine = regexp.MustCompile(`(?m)^\s*pid = (\d+)\s*$`)

// serviceStatus reports whether launchd is running the daemon and where its log is.
func serviceStatus(e *env, cfgFlag string) error {
	plist, err := plistPath()
	if err != nil {
		return err
	}
	out, _ := runExternal("launchctl", "print", serviceTarget())
	_, statErr := os.Stat(plist)
	installed := statErr == nil

	if m := pidLine.FindStringSubmatch(out); m != nil {
		fmt.Fprintf(e.out, "running (pid %s)\n", m[1])
	} else if installed {
		fmt.Fprintln(e.out, "installed, not running")
	} else {
		fmt.Fprintln(e.out, "not installed")
	}

	cfgPath, err := configPath(cfgFlag)
	if err != nil {
		return err
	}
	if cfg := optionalConfig(cfgPath); cfg != nil {
		fmt.Fprintf(e.out, "log: %s\n", filepath.Join(cfg.Server.DataDir, logFileName))
	} else {
		fmt.Fprintln(e.out, "log: unknown (the config does not load)")
	}
	return nil
}

// plistSpec is what the plist template needs.
type plistSpec struct {
	Label   string
	Args    []string
	Path    string
	Env     []envVar // recorded besides PATH
	LogPath string
}

// envVar is one environment variable set in the plist.
type envVar struct {
	Name, Value string
}

// plistText is the launchd property list. Every value goes through xmlEscape.
const plistText = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xmlEscape .Label}}</string>
	<key>ProgramArguments</key>
	<array>
{{- range .Args}}
		<string>{{xmlEscape .}}</string>
{{- end}}
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>{{xmlEscape .Path}}</string>
{{- range .Env}}
		<key>{{xmlEscape .Name}}</key>
		<string>{{xmlEscape .Value}}</string>
{{- end}}
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardOutPath</key>
	<string>{{xmlEscape .LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{xmlEscape .LogPath}}</string>
</dict>
</plist>
`

var plistTemplate = template.Must(template.New("plist").
	Funcs(template.FuncMap{"xmlEscape": xmlEscape}).
	Parse(plistText))

// renderPlist returns the plist for spec. Label defaults to serviceLabel.
func renderPlist(spec plistSpec) (string, error) {
	if spec.Label == "" {
		spec.Label = serviceLabel
	}
	var b bytes.Buffer
	if err := plistTemplate.Execute(&b, spec); err != nil {
		return "", fmt.Errorf("render plist: %w", err)
	}
	return b.String(), nil
}

// xmlEscape escapes s for use as XML character data, including quotes and control whitespace.
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plistPath is where the plist lives: ~/Library/LaunchAgents/io.onlet.serve.plist.
func plistPath() (string, error) {
	if launchAgentsDir == "" {
		return "", errors.New("find home directory for ~/Library/LaunchAgents")
	}
	return filepath.Join(launchAgentsDir, serviceLabel+".plist"), nil
}

// domain is the launchd domain of the logged-in user, gui/<uid>.
func launchdDomain() string {
	return fmt.Sprintf("gui/%d", currentUID())
}

// serviceTarget is the service's full launchd name, gui/<uid>/io.onlet.serve.
func serviceTarget() string {
	return launchdDomain() + "/" + serviceLabel
}

// onletExecutable is the running binary's absolute path with symlinks resolved, so the plist
// keeps working after the symlink (for example a Homebrew link) is re-pointed.
func onletExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find onlet binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve onlet binary: %w", err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve onlet binary: %w", err)
	}
	return abs, nil
}

// runLaunchctl runs launchctl with args and turns a failure into an error that includes its output.
func runLaunchctl(args ...string) (string, error) {
	out, err := runExternal("launchctl", args...)
	if err != nil {
		return out, launchctlError(args, out, err)
	}
	return out, nil
}

func launchctlError(args []string, out string, err error) error {
	msg := strings.TrimSpace(out)
	if msg == "" {
		return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
	}
	return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, msg)
}

// notLoadedOutput reports whether launchctl said the service was not loaded to begin with.
func notLoadedOutput(out string) bool {
	for _, s := range []string{"No such process", "Could not find service", "not loaded", "No such file"} {
		if strings.Contains(out, s) {
			return true
		}
	}
	return false
}
