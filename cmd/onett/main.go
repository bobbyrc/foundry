// Command onett is one binary with two jobs: `onett serve` runs the daemon (API, web UI and
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
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/config"
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
		{"init", "write a starter onett.yaml", cmdInit},
		{"token", "print the API token", cmdToken},
		{"open", "open the web UI in a browser (sign in there)", cmdOpen},
		{"status", "what is running, and what needs you", cmdStatus},
		{"projects", "list managed projects", cmdProjects},
		{"agents", "list agent roles", cmdAgents},
		{"config", "print the redacted config, or --reload it", cmdConfig},
		{"task", "create, list, show, cancel and retry tasks", cmdTask},
		{"run", "list, show, cancel and print logs of agent runs", cmdRun},
		{"deploys", "list releases, or show one with its log: deploys show ID", cmdDeploys},
		{"approvals", "list approvals waiting for a decision", cmdApprovals},
		{"approve", "approve or reject an approval", cmdApprove},
		{"events", "show history, or follow the event stream", cmdEvents},
		{"loops", "list recurring loops", cmdLoops},
		{"loop", "run a loop now: loop run NAME [--project P]", cmdLoop},
		{"project", "add a project to the config, or pause or resume one: project add|pause|resume", cmdProject},
		{"user", "reset a sign-in password on this machine: user reset-password NAME", cmdUser},
		{"pause", "stop starting runs and loops", cmdPause},
		{"resume", "resume a paused factory", cmdResume},
		{"chat", "message the orchestrator and print its reply", cmdChat},
		{"service", "install, uninstall or check the launchd service that keeps serve running", cmdService},
		{"doctor", "check the setup: config, git, gh, harness CLIs, projects and the daemon", cmdDoctor},
		{"version", "print the onett version", cmdVersion},
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
	fmt.Fprintf(stderr, "onett: unknown command %q\n\n", args[0])
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
		fmt.Fprintf(e.errOut, "onett: %s\n", cleanText(ue.msg))
		return 2
	}
	fmt.Fprintf(e.errOut, "onett: %s\n", cleanText(err.Error()))
	return 1
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "onett: a personal software factory.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: onett <command> [flags] [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commandList() {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	fmt.Fprintf(tw, "  %s\t%s\n", "help", "show this help")
	_ = tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Run "onett <command> -h" for a command's flags; for task, run and project, put the verb first: "onett task create -h".`)
	fmt.Fprintln(w, "Environment: ONETT_CONFIG (config file), ONETT_API (server URL),")
	fmt.Fprintln(w, "ONETT_TOKEN (API token). Without them the config file's server address and")
	fmt.Fprintln(w, "data_dir are used.")
}

// Usage text for the commands whose first argument is a verb. They print it for -h, -help, --help
// and help, and exit 0, as service does.
const (
	taskHelp = "usage: onett task create|list|show|cancel|retry|close-conversation [flags] [arguments]\n" +
		"Run \"onett task <verb> -h\" for a verb's flags."
	runHelp = "usage: onett run list|show|cancel|logs [flags] [arguments]\n" +
		"Run \"onett run <verb> -h\" for a verb's flags."
	projectHelp = "usage: onett project add|pause|resume NAME [flags]\n" +
		"Run \"onett project <verb> -h\" for a verb's flags."
	loopHelp = "usage: onett loop run NAME [--project P]"
)

// isHelpFlag reports whether arg asks for help: -h, -help or --help.
func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "-help" || arg == "--help"
}

// usageError marks a mistake in how a command was invoked (exit code 2).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// newFlagSet returns a flag set for one command that reports errors to e.errOut.
func newFlagSet(name string, e *env) *flag.FlagSet {
	fs := flag.NewFlagSet("onett "+name, flag.ContinueOnError)
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
		return usagef("usage: onett %s", usageLine)
	}
	return nil
}

// configPath resolves the config file: the --config flag, then $ONETT_CONFIG, then
// ~/.config/onett/onett.yaml.
func configPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv("ONETT_CONFIG"); v != "" {
		return v, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "onett.yaml"), nil
}

// userConfigDir is ~/.config/onett, where the config file and secrets.env live.
func userConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "onett"), nil
}

// loadConfig loads the config file at path. A missing file is reported with the hint to run
// `onett init`.
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no config at %s: run `onett init` to create one", path)
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

// apiBaseURL is $ONETT_API, else the config's server address, else the default port.
func apiBaseURL(cfg *config.Config) string {
	if v := os.Getenv("ONETT_API"); v != "" {
		return v
	}
	if cfg != nil {
		return config.LocalURL(cfg.Server.Addr)
	}
	return defaultAPIBase
}

// apiToken is $ONETT_TOKEN, else the token file in the config's data directory.
func apiToken(cfg *config.Config) (string, error) {
	if v := os.Getenv("ONETT_TOKEN"); v != "" {
		return v, nil
	}
	if cfg != nil {
		if t, err := readToken(cfg.Server.DataDir); err == nil {
			return t, nil
		}
	}
	return "", errors.New("no token: set ONETT_TOKEN or run on the machine running onett serve")
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
	_, err = fmt.Fprintln(w, escapeC1(string(data)))
	return err
}

// escapeC1 writes the C1 control characters (U+0080 to U+009F) in s as \uXXXX escapes. In JSON
// they can appear only inside strings, so the output stays valid JSON with the same meaning. The
// C0 controls are already escaped by encoding/json.
func escapeC1(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x80 && r <= 0x9f {
			fmt.Fprintf(&b, `\u%04x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// table writes rows under headers, aligned in columns.
func table(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cleanCells(headers), "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(cleanCells(r), "\t"))
	}
	_ = tw.Flush()
}

// pausedColumn is the PAUSED cell of `onett projects`: "config" or "owner" for the one who paused
// the project, "-" when it runs. A paused project that names no pauser shows "yes".
func pausedColumn(p client.ProjectView) string {
	switch {
	case p.PausedBy != "":
		return oneLine(p.PausedBy)
	case p.Paused:
		return "yes"
	}
	return "-"
}

// terminalEscape matches the escape sequences a terminal acts on: OSC (ESC ], or the C1 byte
// U+009D) up to BEL or ST, CSI (ESC [, or U+009B) with its parameters, and the two-byte ESC
// sequences. A bare ESC is left for cleanText's control-character pass.
var terminalEscape = regexp.MustCompile(
	`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?` +
		`|\x{9d}[^\x07\x{9c}]*(?:\x07|\x{9c})?` +
		`|\x1b\[[0-?]*[ -/]*[@-~]` +
		`|\x{9b}[0-?]*[ -/]*[@-~]` +
		`|\x1b[ -/]*[0-~]`)

// cleanText makes text from the server, an agent or GitHub safe to print to a terminal. It removes
// terminal escape sequences, then every control character except \n and \t, including DEL and
// the C1 range U+0080 to U+009F. Line breaks and tabs stay, for text printed over several lines.
func cleanText(s string) string {
	s = terminalEscape.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		}
		return r
	}, s)
}

// oneLine is cleanText with every line break and tab turned into a space, for a value that must
// stay on one line: a table cell, or a field of a line.
func oneLine(s string) string {
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(cleanText(s))
}

// cleanCells applies oneLine to every cell, so a cell can neither break the table nor move the
// cursor.
func cleanCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = oneLine(c)
	}
	return out
}

// truncate shortens s to at most n runes, ending with "..." when it cuts. The result is cleaned
// and on one line.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(cleanText(s)), " ")
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
