// Command foundry is one binary with two jobs: `foundry serve` runs the daemon (API, web UI and
// the engine that runs agents), and every other command is a client of the daemon's HTTP API.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/foundry/internal/client"
	"github.com/bobbyrc/foundry/internal/config"
)

// version is set by the release build with -ldflags "-X main.version=...".
var version = "dev"

const defaultAPIBase = "http://127.0.0.1:7777"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// env is what every command writes to.
type env struct {
	out    io.Writer
	errOut io.Writer
}

// command is one entry of the command list. run receives the arguments after the command name.
type command struct {
	name    string
	summary string
	run     func(e *env, args []string) error
}

// commandList is the single source of truth for dispatch and for the usage text.
func commandList() []command {
	return []command{
		{"serve", "run the daemon: API, web UI and engine", cmdServe},
		{"init", "write a starter foundry.yaml", cmdInit},
		{"token", "print the API token", cmdToken},
		{"open", "open the web UI in a browser", cmdOpen},
		{"status", "what is running, and what needs you", cmdStatus},
		{"projects", "list managed projects", cmdProjects},
		{"agents", "list agent roles", cmdAgents},
		{"config", "print the redacted config, or --reload it", cmdConfig},
		{"task", "create, list, show, cancel and retry tasks", cmdTask},
		{"run", "list, show, cancel and tail agent runs", cmdRun},
		{"approvals", "list approvals waiting for a decision", cmdApprovals},
		{"approve", "approve or reject an approval", cmdApprove},
		{"events", "show history, or follow the event stream", cmdEvents},
		{"loops", "list recurring loops", cmdLoops},
		{"loop", "run a loop now: loop run NAME [--project P]", cmdLoop},
		{"pause", "stop starting runs and loops", cmdPause},
		{"resume", "resume a paused factory", cmdResume},
		{"chat", "message the orchestrator and print its reply", cmdChat},
		{"service", "install, uninstall or check the launchd service that keeps serve running", cmdService},
		{"doctor", "check the setup: config, git, gh, harness CLIs, projects and the daemon", cmdDoctor},
		{"version", "print the foundry version", cmdVersion},
	}
}

// run dispatches one invocation and returns the process exit code: 0 on success, 1 on a
// runtime error and 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	e := &env{out: stdout, errOut: stderr}
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return 0
	}
	for _, c := range commandList() {
		if c.name != args[0] {
			continue
		}
		err := c.run(e, args[1:])
		return exitCode(e, err)
	}
	fmt.Fprintf(stderr, "foundry: unknown command %q\n\n", args[0])
	usage(stderr)
	return 2
}

// exitCode prints err, if any, and maps it to an exit code.
func exitCode(e *env, err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	}
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(e.errOut, "foundry: %s\n", ue.msg)
		return 2
	}
	fmt.Fprintf(e.errOut, "foundry: %s\n", err)
	return 1
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "foundry: a personal software factory.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: foundry <command> [flags] [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commandList() {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	fmt.Fprintf(tw, "  %s\t%s\n", "help", "show this help")
	_ = tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Run "foundry <command> -h" for a command's flags.`)
	fmt.Fprintln(w, "Environment: FOUNDRY_CONFIG (config file), FOUNDRY_API (server URL),")
	fmt.Fprintln(w, "FOUNDRY_TOKEN (API token). Without them the config file's server address and")
	fmt.Fprintln(w, "data_dir are used.")
}

// usageError marks a mistake in how a command was invoked (exit code 2).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// newFlagSet returns a flag set for one command that reports errors to e.errOut.
func newFlagSet(name string, e *env) *flag.FlagSet {
	fs := flag.NewFlagSet("foundry "+name, flag.ContinueOnError)
	fs.SetOutput(e.errOut)
	return fs
}

// parseArgs parses flags that may appear before, between or after positional arguments, which
// the standard flag package does not allow. It returns the positional arguments in order.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usagef("%s", err)
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// requireArgs fails with a usage error unless exactly n positional arguments were given.
func requireArgs(args []string, n int, usageLine string) error {
	if len(args) != n {
		return usagef("usage: foundry %s", usageLine)
	}
	return nil
}

// configPath resolves the config file: the --config flag, then $FOUNDRY_CONFIG, then
// ~/.config/foundry/foundry.yaml.
func configPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv("FOUNDRY_CONFIG"); v != "" {
		return v, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "foundry.yaml"), nil
}

// userConfigDir is ~/.config/foundry, where the config file and secrets.env live.
func userConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "foundry"), nil
}

// loadConfig loads the config file at path. A missing file is reported with the hint to run
// `foundry init`.
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no config at %s: run `foundry init` to create one", path)
		}
		return nil, err
	}
	return cfg, nil
}

// optionalConfig returns the config at path, or nil when it does not load. The API commands use
// it only for defaults, so a broken file must not stop them from reaching a running server.
func optionalConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	return cfg
}

// apiBaseURL is $FOUNDRY_API, else the config's server address, else the default port.
func apiBaseURL(cfg *config.Config) string {
	if v := os.Getenv("FOUNDRY_API"); v != "" {
		return v
	}
	if cfg != nil {
		return "http://" + cfg.Server.Addr
	}
	return defaultAPIBase
}

// apiToken is $FOUNDRY_TOKEN, else the token file in the config's data directory.
func apiToken(cfg *config.Config) (string, error) {
	if v := os.Getenv("FOUNDRY_TOKEN"); v != "" {
		return v, nil
	}
	if cfg != nil {
		if t, err := readToken(cfg.Server.DataDir); err == nil {
			return t, nil
		}
	}
	return "", errors.New("no token: set FOUNDRY_TOKEN or run on the machine running foundry serve")
}

// readToken returns the trimmed contents of <dataDir>/token.
func readToken(dataDir string) (string, error) {
	raw, err := os.ReadFile(tokenPath(dataDir))
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(raw))
	if t == "" {
		return "", fmt.Errorf("token file %s is empty", tokenPath(dataDir))
	}
	return t, nil
}

func tokenPath(dataDir string) string {
	return filepath.Join(dataDir, "token")
}

// apiClient builds the client for the API commands, which resolve the server from the
// environment and the default config file.
func apiClient() (*client.Client, error) {
	path, err := configPath("")
	if err != nil {
		return nil, err
	}
	cfg := optionalConfig(path)
	token, err := apiToken(cfg)
	if err != nil {
		return nil, err
	}
	return client.New(apiBaseURL(cfg), token), nil
}

// printJSON writes v as indented JSON, the --json output of list and show commands.
func printJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// table writes rows under headers, aligned in columns.
func table(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}

// truncate shortens s to at most n runes, ending with "..." when it cuts.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n <= 3 {
		return string([]rune(s)[:n])
	}
	return string([]rune(s)[:n-3]) + "..."
}

// clock formats a time as local HH:MM:SS, or "-" for the zero time.
func clock(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("15:04:05")
}

// stamp formats a time as local date and time to the minute, or "-" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// orDash returns s, or "-" when s is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// joinNonZero renders a count map as "queued=1 running=2" in sorted key order, skipping zeros.
func joinNonZero(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(counts))
	for k, v := range counts {
		if v != 0 {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "-"
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, " ")
}
