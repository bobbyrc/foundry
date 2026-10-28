package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/client"
	"github.com/bobbyrc/foundry/internal/domain"
)

// Timings for the commands that wait on the server. They are variables so tests can shorten
// them.
var (
	logPollInterval   = 2 * time.Second
	eventRetryDelay   = 2 * time.Second
	chatPollInterval  = 2 * time.Second
	chatReplyDeadline = 30 * time.Minute
)

// logPageSize is how many log lines one request asks for.
const logPageSize = 1000

// titleWidth is the longest task title shown in a table.
const titleWidth = 60

// splitList turns "a,b" into []string{"a","b"}; an empty string gives nil.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- status ----

// statusSnapshot is what `foundry status` gathers in one pass.
type statusSnapshot struct {
	Stats     client.Stats        `json:"stats"`
	Control   client.ControlState `json:"control"`
	Approvals []domain.Approval   `json:"approvals"`
	Blocked   []domain.Task       `json:"blocked"`
	Running   []domain.Run        `json:"running"`
}

func cmdStatus(e *env, args []string) error {
	fs := newFlagSet("status", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "status [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	var snap statusSnapshot
	if snap.Stats, err = c.Stats(ctx); err != nil {
		return err
	}
	if snap.Control, err = c.Control(ctx); err != nil {
		return err
	}
	if snap.Approvals, err = c.ListApprovals(ctx, "pending"); err != nil {
		return err
	}
	if snap.Blocked, err = c.ListTasks(ctx, client.TaskFilter{Status: []string{"blocked"}}); err != nil {
		return err
	}
	if snap.Running, err = c.ListRuns(ctx, client.RunFilter{Status: []string{"starting", "running"}}); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, snap)
	}
	return writeStatus(e.out, snap)
}

// costToday renders today's cost, against the daily budget when one is set.
func costToday(s client.Stats) string {
	out := fmt.Sprintf("$%.2f", s.CostTodayUSD)
	if s.DailyBudgetUSD > 0 {
		out += fmt.Sprintf(" of $%.2f daily budget", s.DailyBudgetUSD)
		if s.CostTodayUSD >= s.DailyBudgetUSD {
			out += " (budget reached — no new runs until midnight)"
		}
	}
	return out
}

func writeStatus(w io.Writer, s statusSnapshot) error {
	if s.Control.Paused {
		fmt.Fprintln(w, "Factory: PAUSED. No runs start and no loops act until `foundry resume`.")
	} else {
		fmt.Fprintln(w, "Factory: running.")
	}
	fmt.Fprintf(w, "Tasks: %s\n", joinNonZero(s.Stats.TasksByStatus))
	fmt.Fprintf(w, "Active runs: %d   Pending approvals: %d   Cost today: %s\n\n",
		s.Stats.ActiveRuns, s.Stats.ApprovalsPending, costToday(s.Stats))

	fmt.Fprintln(w, "Needs you:")
	n := 0
	for _, a := range s.Approvals {
		fmt.Fprintf(w, "  approval %s  [%s] %s: %s\n", a.ID, a.Kind, oneLine(a.Title), orDash(oneLine(a.Reason)))
		n++
	}
	for _, t := range s.Blocked {
		fmt.Fprintf(w, "  blocked  %s  %s: %s\n", oneLine(t.ID), truncate(t.Title, titleWidth), orDash(oneLine(t.BlockedReason)))
		n++
	}
	if n == 0 {
		fmt.Fprintln(w, "  nothing")
	}

	fmt.Fprintln(w, "\nRunning:")
	if len(s.Running) == 0 {
		fmt.Fprintln(w, "  nothing")
	}
	for _, r := range s.Running {
		fmt.Fprintf(w, "  %-12s %-16s run %s  task %s\n", oneLine(r.Role), oneLine(r.Project), oneLine(r.ID), oneLine(r.TaskID))
	}
	return nil
}

// ---- projects, agents, config, version ----

func cmdProjects(e *env, args []string) error {
	fs := newFlagSet("projects", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "projects [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	projects, err := c.Projects(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, projects)
	}
	rows := make([][]string, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, []string{p.Name, orDash(p.Repo), pausedColumn(p), joinNonZero(p.Counts)})
	}
	table(e.out, []string{"NAME", "REPO", "PAUSED", "TASKS"}, rows)
	return nil
}

func cmdAgents(e *env, args []string) error {
	fs := newFlagSet("agents", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "agents [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	agents, err := c.Agents(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, agents)
	}
	rows := make([][]string, 0, len(agents))
	for _, a := range agents {
		rows = append(rows, []string{
			a.Name, orDash(a.Harness), orDash(a.Model), orDash(strings.Join(a.Kinds, ",")), truncate(a.Description, 70),
		})
	}
	table(e.out, []string{"ROLE", "HARNESS", "MODEL", "KINDS", "DESCRIPTION"}, rows)
	return nil
}

func cmdConfig(e *env, args []string) error {
	fs := newFlagSet("config", e)
	reload := fs.Bool("reload", false, "re-read the config file on the server")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "config [--reload]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if *reload {
		if err := c.ReloadConfig(ctx); err != nil {
			return err
		}
		fmt.Fprintln(e.out, "config reloaded")
		return nil
	}
	cfg, err := c.Config(ctx)
	if err != nil {
		return err
	}
	return printJSON(e.out, cfg)
}

func cmdVersion(e *env, args []string) error {
	if err := requireArgs(args, 0, "version"); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "foundry %s\n", version)
	return nil
}

// ---- tasks ----

func cmdTask(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: foundry task create|list|show|cancel|retry ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "create":
		return taskCreate(e, rest)
	case "list":
		return taskList(e, rest)
	case "show":
		return taskShow(e, rest)
	case "cancel":
		return taskCancel(e, rest)
	case "retry":
		return taskRetry(e, rest)
	}
	return usagef("unknown task command %q (want create, list, show, cancel or retry)", sub)
}

func taskCreate(e *env, args []string) error {
	fs := newFlagSet("task create", e)
	project := fs.String("project", "", "project name (required)")
	kind := fs.String("kind", "", "task kind: feature, bugfix, issue, chore, improve, question, incident (required)")
	title := fs.String("title", "", "one-line title (required)")
	body := fs.String("body", "", "task body")
	bodyFile := fs.String("body-file", "", "read the body from this file ('-' for stdin)")
	role := fs.String("role", "", "agent role (default: routed by kind)")
	priority := fs.Int("priority", 0, "higher runs first")
	parent := fs.String("parent", "", "parent task ID")
	jsonOut := fs.Bool("json", false, "print the created task as JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "task create --project P --kind K --title T [--body B | --body-file F]"); err != nil {
		return err
	}
	if *project == "" || *kind == "" || *title == "" {
		return usagef("task create needs --project, --kind and --title")
	}
	if *body != "" && *bodyFile != "" {
		return usagef("use --body or --body-file, not both")
	}
	text := *body
	if *bodyFile != "" {
		data, err := readBodyFile(*bodyFile)
		if err != nil {
			return err
		}
		text = string(data)
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	task, err := c.CreateTask(context.Background(), client.NewTask{
		Project: *project, Kind: *kind, Title: *title, Body: text, Role: *role,
		Priority: *priority, ParentID: *parent, Source: "api",
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, task)
	}
	fmt.Fprintln(e.out, task.ID)
	return nil
}

func readBodyFile(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

func taskList(e *env, args []string) error {
	fs := newFlagSet("task list", e)
	project := fs.String("project", "", "only this project")
	status := fs.String("status", "", "comma-separated statuses, e.g. queued,running")
	kind := fs.String("kind", "", "only this task kind")
	limit := fs.Int("limit", 0, "maximum rows (default 200)")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "task list [--project P] [--status a,b] [--kind K] [--limit N]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	tasks, err := c.ListTasks(context.Background(), client.TaskFilter{
		Project: *project, Status: splitList(*status), Kind: *kind, Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, tasks)
	}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{t.ID, string(t.Status), string(t.Kind), t.Project, truncate(t.Title, titleWidth)})
	}
	table(e.out, []string{"ID", "STATUS", "KIND", "PROJECT", "TITLE"}, rows)
	return nil
}

func taskShow(e *env, args []string) error {
	fs := newFlagSet("task show", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "task show ID [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	d, err := c.GetTask(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, d)
	}
	writeTaskDetail(e.out, d)
	return nil
}

func writeTaskDetail(w io.Writer, d client.TaskDetail) {
	t := d.Task
	fmt.Fprintf(w, "Task %s: %s\n", oneLine(t.ID), oneLine(t.Title))
	fmt.Fprintf(w, "Status:    %s\n", oneLine(string(t.Status)))
	fmt.Fprintf(w, "Kind:      %s   Role: %s   Project: %s\n", oneLine(string(t.Kind)), orDash(oneLine(t.Role)), oneLine(t.Project))
	fmt.Fprintf(w, "Priority:  %d   Attempts: %d of %d   Source: %s\n", t.Priority, t.Attempts, t.MaxAttempts, orDash(oneLine(t.Source)))
	risk := "-"
	if t.Risk != "" {
		risk = oneLine(string(t.Risk))
		if len(t.RiskReasons) > 0 {
			risk += " (" + oneLine(strings.Join(t.RiskReasons, "; ")) + ")"
		}
	}
	fmt.Fprintf(w, "Risk:      %s\n", risk)
	if t.PRNumber > 0 {
		fmt.Fprintf(w, "PR:        #%d %s\n", t.PRNumber, oneLine(t.PRURL))
	}
	if t.Branch != "" {
		fmt.Fprintf(w, "Branch:    %s\n", oneLine(t.Branch))
	}
	if t.BlockedReason != "" {
		fmt.Fprintf(w, "Blocked:   %s\n", oneLine(t.BlockedReason))
	}
	if t.Summary != "" {
		fmt.Fprintf(w, "Summary:   %s\n", oneLine(t.Summary))
	}
	if t.ParentID != "" {
		fmt.Fprintf(w, "Parent:    %s\n", oneLine(t.ParentID))
	}
	fmt.Fprintf(w, "Created:   %s   Updated: %s\n", stamp(t.CreatedAt), stamp(t.UpdatedAt))
	if t.Body != "" {
		fmt.Fprintf(w, "\nBody:\n%s\n", cleanText(strings.TrimRight(t.Body, "\n")))
	}

	fmt.Fprintln(w, "\nRuns:")
	if len(d.Runs) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		rows := make([][]string, 0, len(d.Runs))
		for _, r := range d.Runs {
			rows = append(rows, []string{r.ID, string(r.Status), r.Role, stamp(r.StartedAt), truncate(r.Error, 50)})
		}
		fmt.Fprint(w, indentTable([]string{"ID", "STATUS", "ROLE", "STARTED", "ERROR"}, rows))
	}
	if len(d.Approvals) > 0 {
		fmt.Fprintln(w, "\nApprovals:")
		for _, a := range d.Approvals {
			fmt.Fprintf(w, "  %s  %s  %s\n", oneLine(a.ID), oneLine(string(a.Status)), oneLine(a.Title))
		}
	}
	if len(d.Children) > 0 {
		fmt.Fprintln(w, "\nChildren:")
		rows := make([][]string, 0, len(d.Children))
		for _, c := range d.Children {
			rows = append(rows, []string{c.ID, string(c.Status), string(c.Kind), truncate(c.Title, titleWidth)})
		}
		fmt.Fprint(w, indentTable([]string{"ID", "STATUS", "KIND", "TITLE"}, rows))
	}
}

// indentTable renders a table with two leading spaces on every line, for nested sections.
func indentTable(headers []string, rows [][]string) string {
	var b strings.Builder
	table(&b, headers, rows)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n") + "\n"
}

func taskCancel(e *env, args []string) error {
	positional, err := parseArgs(newFlagSet("task cancel", e), args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "task cancel ID"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if err := c.CancelTask(context.Background(), positional[0]); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "cancelled task %s\n", positional[0])
	return nil
}

func taskRetry(e *env, args []string) error {
	fs := newFlagSet("task retry", e)
	note := fs.String("note", "", "guidance appended to the task body for the next attempt")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "task retry ID [--note N]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if err := c.RetryTask(context.Background(), positional[0], *note); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "retried task %s\n", positional[0])
	return nil
}

// ---- runs ----

func cmdRun(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: foundry run list|show|cancel|logs ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runList(e, rest)
	case "show":
		return runShow(e, rest)
	case "cancel":
		return runCancel(e, rest)
	case "logs":
		return runLogs(e, rest)
	}
	return usagef("unknown run command %q (want list, show, cancel or logs)", sub)
}

func runList(e *env, args []string) error {
	fs := newFlagSet("run list", e)
	task := fs.String("task", "", "only this task")
	project := fs.String("project", "", "only this project")
	status := fs.String("status", "", "comma-separated statuses")
	limit := fs.Int("limit", 0, "maximum rows (default 200)")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "run list [--task ID] [--project P] [--status S]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	runs, err := c.ListRuns(context.Background(), client.RunFilter{
		TaskID: *task, Project: *project, Status: splitList(*status), Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, runs)
	}
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		rows = append(rows, []string{r.ID, string(r.Status), r.Role, r.Project, r.TaskID, stamp(r.StartedAt)})
	}
	table(e.out, []string{"ID", "STATUS", "ROLE", "PROJECT", "TASK", "STARTED"}, rows)
	return nil
}

func runShow(e *env, args []string) error {
	fs := newFlagSet("run show", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "run show ID [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	r, err := c.GetRun(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, r)
	}
	fmt.Fprintf(e.out, "Run %s\n", oneLine(r.ID))
	fmt.Fprintf(e.out, "Status:   %s\n", oneLine(string(r.Status)))
	fmt.Fprintf(e.out, "Task:     %s   Project: %s   Role: %s\n", oneLine(r.TaskID), oneLine(r.Project), orDash(oneLine(r.Role)))
	fmt.Fprintf(e.out, "Harness:  %s   Model: %s\n", orDash(oneLine(r.Harness)), orDash(oneLine(r.Model)))
	fmt.Fprintf(e.out, "Branch:   %s\n", orDash(oneLine(r.Branch)))
	fmt.Fprintf(e.out, "Workdir:  %s\n", orDash(oneLine(r.Workdir)))
	fmt.Fprintf(e.out, "Started:  %s   Ended: %s\n", stamp(r.StartedAt), stamp(derefTime(r.EndedAt)))
	fmt.Fprintf(e.out, "Exit:     %d   Cost: $%.4f   Tokens in/out: %d/%d\n", r.ExitCode, r.CostUSD, r.InputTokens, r.OutputTokens)
	if r.Summary != "" {
		fmt.Fprintf(e.out, "Summary:  %s\n", cleanText(r.Summary))
	}
	if r.Error != "" {
		fmt.Fprintf(e.out, "Error:    %s\n", cleanText(r.Error))
	}
	return nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func runCancel(e *env, args []string) error {
	positional, err := parseArgs(newFlagSet("run cancel", e), args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "run cancel ID"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if err := c.CancelRun(context.Background(), positional[0]); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "cancelled run %s\n", positional[0])
	return nil
}

func runLogs(e *env, args []string) error {
	fs := newFlagSet("run logs", e)
	follow := fs.Bool("follow", false, "keep printing new lines until the run finishes")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "run logs ID [--follow]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	id := positional[0]
	ctx := context.Background()
	if !*follow {
		_, err := printLogs(ctx, e.out, c, id, 0)
		return err
	}

	ctx, stop := signalContext()
	defer stop()
	after, err := printLogs(ctx, e.out, c, id, 0)
	if err != nil {
		return ignoreInterrupt(ctx, err)
	}
	ticker := time.NewTicker(logPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		// Read the status before the logs: a run that has finished has written all its lines,
		// so the log read after it sees everything.
		run, err := c.GetRun(ctx, id)
		if err != nil {
			return ignoreInterrupt(ctx, err)
		}
		after, err = printLogs(ctx, e.out, c, id, after)
		if err != nil {
			return ignoreInterrupt(ctx, err)
		}
		if !runActive(run.Status) {
			fmt.Fprintf(e.out, "run %s finished: %s\n", id, run.Status)
			return nil
		}
	}
}

// printLogs prints every log line of run after seq and returns the last seq seen.
func printLogs(ctx context.Context, w io.Writer, c *client.Client, runID string, after int64) (int64, error) {
	for {
		lines, err := c.RunLogs(ctx, runID, after, logPageSize)
		if err != nil {
			return after, err
		}
		for _, l := range lines {
			fmt.Fprintf(w, "%s %s %s\n", clock(l.Time), oneLine(string(l.Kind)), cleanText(l.Text))
			after = l.Seq
		}
		if len(lines) < logPageSize {
			return after, nil
		}
	}
}

func runActive(s domain.RunStatus) bool {
	return s == domain.RunStarting || s == domain.RunRunning
}

// ignoreInterrupt turns the error of a request interrupted by Ctrl-C into a clean exit.
func ignoreInterrupt(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// ---- approvals ----

func cmdApprovals(e *env, args []string) error {
	fs := newFlagSet("approvals", e)
	all := fs.Bool("all", false, "include decided approvals")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "approvals [--all] [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	status := "pending"
	if *all {
		status = "all"
	}
	approvals, err := c.ListApprovals(context.Background(), status)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, approvals)
	}
	headers := []string{"ID", "KIND", "PROJECT", "TITLE", "REASON"}
	if *all {
		headers = append([]string{"ID", "STATUS"}, headers[1:]...)
	}
	rows := make([][]string, 0, len(approvals))
	for _, a := range approvals {
		row := []string{a.ID, a.Kind, a.Project, truncate(a.Title, titleWidth), truncate(a.Reason, 60)}
		if *all {
			row = []string{a.ID, string(a.Status), a.Kind, a.Project, truncate(a.Title, titleWidth), truncate(a.Reason, 60)}
		}
		rows = append(rows, row)
	}
	table(e.out, headers, rows)
	return nil
}

func cmdApprove(e *env, args []string) error {
	fs := newFlagSet("approve", e)
	reject := fs.Bool("reject", false, "reject instead of approving")
	note := fs.String("note", "", "note recorded with the decision")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "approve ID [--reject] [--note N]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	a, err := c.DecideApproval(context.Background(), positional[0], !*reject, *note)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.out, "approval %s %s (task %s)\n", a.ID, a.Status, a.TaskID)
	return nil
}

// ---- events ----

func cmdEvents(e *env, args []string) error {
	fs := newFlagSet("events", e)
	follow := fs.Bool("follow", false, "keep printing new events")
	after := fs.Int64("after", 0, "only events with an ID greater than this")
	limit := fs.Int("limit", 0, "maximum events in the history (default 200)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "events [--follow] [--after N] [--limit N]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if !*follow {
		evs, err := c.EventHistory(context.Background(), client.EventFilter{After: *after, Limit: *limit})
		if err != nil {
			return err
		}
		for _, ev := range evs {
			writeEvent(e.out, ev)
		}
		return nil
	}

	ctx, stop := signalContext()
	defer stop()
	last := *after
	for {
		err := c.Events(ctx, last, func(ev domain.Event) {
			writeEvent(e.out, ev)
			if ev.ID > last {
				last = ev.ID
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			return err
		}
		if err != nil {
			fmt.Fprintf(e.errOut, "event stream lost (%s); reconnecting\n", cleanText(err.Error()))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(eventRetryDelay):
		}
	}
}

func writeEvent(w io.Writer, ev domain.Event) {
	fmt.Fprintf(w, "%s %-22s %s\n", clock(ev.Time), oneLine(ev.Type), oneLine(ev.Message))
}

// ---- loops ----

func cmdLoops(e *env, args []string) error {
	fs := newFlagSet("loops", e)
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "loops [--json]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	loops, err := c.Loops(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, loops)
	}
	rows := make([][]string, 0, len(loops))
	for _, l := range loops {
		enabled := "no"
		if l.Enabled {
			enabled = "yes"
		}
		rows = append(rows, []string{l.Name, orDash(l.Project), enabled, l.Every, stamp(l.LastRunAt), truncate(l.LastError, 60)})
	}
	table(e.out, []string{"NAME", "PROJECT", "ENABLED", "EVERY", "LAST RUN", "LAST ERROR"}, rows)
	return nil
}

func cmdLoop(e *env, args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return usagef("usage: foundry loop run NAME [--project P]")
	}
	fs := newFlagSet("loop run", e)
	project := fs.String("project", "", "run the loop for this project only (default: every project)")
	positional, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "loop run NAME [--project P]"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	if err := c.RunLoop(context.Background(), positional[0], *project); err != nil {
		return err
	}
	if *project != "" {
		fmt.Fprintf(e.out, "started loop %s for %s\n", positional[0], *project)
	} else {
		fmt.Fprintf(e.out, "started loop %s\n", positional[0])
	}
	return nil
}

// ---- pause, resume ----

// cmdProject pauses or resumes one project: `foundry project pause NAME`.
func cmdProject(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: foundry project pause|resume NAME")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "pause", "resume":
		return setProjectPaused(e, sub, rest)
	}
	return usagef("unknown project command %q (want pause or resume)", sub)
}

func setProjectPaused(e *env, sub string, args []string) error {
	if err := requireArgs(args, 1, "project "+sub+" NAME"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	name := args[0]
	ctx := context.Background()
	if sub == "resume" {
		if _, err := c.ResumeProject(ctx, name); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "project %s resumed\n", oneLine(name))
		return nil
	}
	state, err := c.PauseProject(ctx, name)
	if err != nil {
		return err
	}
	msg := "paused"
	if state.PausedBy != "" {
		msg += " (" + oneLine(state.PausedBy) + ")"
	}
	fmt.Fprintf(e.out, "project %s %s\n", oneLine(name), msg)
	return nil
}

func cmdPause(e *env, args []string) error {
	return setPaused(e, args, true)
}

func cmdResume(e *env, args []string) error {
	return setPaused(e, args, false)
}

func setPaused(e *env, args []string, pause bool) error {
	name := "resume"
	if pause {
		name = "pause"
	}
	if err := requireArgs(args, 0, name); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if pause {
		_, err = c.Pause(ctx)
		fmt.Fprintln(e.out, "paused: no runs start and no loops act until `foundry resume`")
	} else {
		_, err = c.Resume(ctx)
		fmt.Fprintln(e.out, "resumed")
	}
	return err
}

// ---- chat ----

func cmdChat(e *env, args []string) error {
	fs := newFlagSet("chat", e)
	conversation := fs.String("conversation", "", "continue this conversation (default: start a new one)")
	project := fs.String("project", "", "default project for work the orchestrator delegates")
	words, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	// words holds every positional argument in order, so the message may be given unquoted.
	message := strings.TrimSpace(strings.Join(words, " "))
	if message == "" {
		return usagef("usage: foundry chat [--conversation ID] [--project P] MESSAGE...")
	}
	c, err := apiClient()
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()
	conv := *conversation
	if conv == "" {
		created, err := c.CreateConversation(ctx, truncate(message, 60), *project)
		if err != nil {
			return err
		}
		conv = created.ID
		fmt.Fprintf(e.errOut, "conversation: %s\n", conv)
	}
	if _, err := c.SendMessage(ctx, conv, message); err != nil {
		return err
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, chatReplyDeadline)
	defer cancel()
	for {
		select {
		case <-deadlineCtx.Done():
			if ctx.Err() != nil {
				return fmt.Errorf("interrupted; the reply is still coming: foundry chat --conversation %s", conv)
			}
			return fmt.Errorf("no reply after %s; check the conversation in the web UI (%s)", chatReplyDeadline, conv)
		case <-time.After(chatPollInterval):
		}
		detail, err := c.GetConversation(deadlineCtx, conv)
		if err != nil {
			if deadlineCtx.Err() != nil {
				continue
			}
			return err
		}
		if detail.Conversation.Busy {
			continue
		}
		fmt.Fprintln(e.out, cleanText(newestAssistantText(detail.Messages)))
		return nil
	}
}

// newestAssistantText returns the text of the assistant message with the latest CreatedAt.
func newestAssistantText(msgs []domain.ChatMessage) string {
	var best *domain.ChatMessage
	for i := range msgs {
		m := &msgs[i]
		if m.Role != "assistant" {
			continue
		}
		if best == nil || !m.CreatedAt.Before(best.CreatedAt) {
			best = m
		}
	}
	if best == nil {
		return "(the orchestrator sent no reply)"
	}
	return best.Text
}
