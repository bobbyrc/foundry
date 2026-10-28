// Package github implements scm.SCM by running the gh CLI (GitHub's official command-line tool).
// Every call goes through a Runner, so tests can replace gh with canned output.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/foundry/internal/scm"
)

// Runner runs one gh command with stdin and returns its stdout. Args are passed as-is; stdin is
// for bodies sent with --body-file -.
type Runner func(ctx context.Context, stdin string, args ...string) ([]byte, error)

// maxCILogRuns caps how many failed runs CILog fetches logs for.
const maxCILogRuns = 3

// GitHub is a scm.SCM scoped to one repository.
type GitHub struct {
	Repo string // "owner/name"
	run  Runner

	mu     sync.Mutex
	labels map[string]bool // labels already ensured to exist in this process
}

var _ scm.SCM = (*GitHub)(nil)

// New returns a GitHub SCM for repo that runs the real gh binary.
func New(repo string) *GitHub {
	return NewWithRunner(repo, ghRunner)
}

// NewWithRunner returns a GitHub SCM for repo that runs gh through r.
func NewWithRunner(repo string, r Runner) *GitHub {
	return &GitHub{Repo: repo, run: r}
}

// ghRunner runs the gh binary. It disables prompts and update notices, and runs in the temp dir
// so gh never picks up a repository from the current checkout.
func ghRunner(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err,
			strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// OpenPR creates a pull request, adds its labels and returns it.
func (g *GitHub) OpenPR(ctx context.Context, spec scm.PRSpec) (scm.PR, error) {
	args := []string{"pr", "create", "--repo", g.Repo, "--head", spec.Head, "--base", spec.Base,
		"--title", spec.Title, "--body-file", "-"}
	if spec.Draft {
		args = append(args, "--draft")
	}
	out, err := g.run(ctx, spec.Body, args...)
	if err != nil {
		return scm.PR{}, fmt.Errorf("open PR %s -> %s: %w", spec.Head, spec.Base, err)
	}
	prURL := lastLine(out)
	number, err := numberFromURL(prURL)
	if err != nil {
		return scm.PR{}, fmt.Errorf("open PR %s: %w", spec.Head, err)
	}
	if len(spec.Labels) > 0 {
		if err := g.EditLabels(ctx, number, spec.Labels, nil); err != nil {
			return scm.PR{}, fmt.Errorf("label PR #%d: %w", number, err)
		}
	}
	return g.GetPR(ctx, number)
}

// prJSON is the subset of `gh pr view --json` that foundry reads.
type prJSON struct {
	Number            int         `json:"number"`
	URL               string      `json:"url"`
	State             string      `json:"state"`
	HeadRefName       string      `json:"headRefName"`
	HeadRefOid        string      `json:"headRefOid"`
	MergeCommit       *commitJSON `json:"mergeCommit"`
	Mergeable         string      `json:"mergeable"`
	Labels            []nameJSON  `json:"labels"`
	StatusCheckRollup []checkJSON `json:"statusCheckRollup"`
}

type commitJSON struct {
	Oid string `json:"oid"`
}

type nameJSON struct {
	Name string `json:"name"`
}

// checkJSON is one entry of statusCheckRollup: a CheckRun (a GitHub Actions job) or a
// StatusContext (a commit status posted by an external system).
type checkJSON struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Context    string `json:"context"`
	State      string `json:"state"`
}

// GetPR fetches one pull request with its state, merge commit, labels and CI status.
func (g *GitHub) GetPR(ctx context.Context, number int) (scm.PR, error) {
	out, err := g.run(ctx, "", "pr", "view", strconv.Itoa(number), "--repo", g.Repo,
		"--json", "number,url,state,headRefName,headRefOid,mergeCommit,mergeable,labels,statusCheckRollup")
	if err != nil {
		return scm.PR{}, fmt.Errorf("view PR #%d: %w", number, err)
	}
	var raw prJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return scm.PR{}, fmt.Errorf("decode PR #%d: %w", number, err)
	}

	ci, failing := ciFromChecks(raw.StatusCheckRollup)
	pr := scm.PR{
		Number:     raw.Number,
		URL:        raw.URL,
		State:      prState(raw.State),
		HeadBranch: raw.HeadRefName,
		HeadSHA:    raw.HeadRefOid,
		Mergeable:  mergeable(raw.Mergeable),
		CI:         ci,
		FailingCI:  failing,
	}
	if raw.MergeCommit != nil {
		pr.MergeSHA = raw.MergeCommit.Oid
	}
	for _, l := range raw.Labels {
		pr.Labels = append(pr.Labels, l.Name)
	}
	return pr, nil
}

func prState(s string) string {
	switch s {
	case "OPEN":
		return "open"
	case "CLOSED":
		return "closed"
	case "MERGED":
		return "merged"
	}
	return strings.ToLower(s)
}

func mergeable(s string) string {
	switch s {
	case "MERGEABLE":
		return "mergeable"
	case "CONFLICTING":
		return "conflicting"
	}
	return "unknown"
}

// failedConclusions are CheckRun conclusions that count as a failing check.
var failedConclusions = map[string]bool{
	"FAILURE":         true,
	"TIMED_OUT":       true,
	"CANCELLED":       true,
	"ACTION_REQUIRED": true,
	"STARTUP_FAILURE": true,
}

// ciFromChecks reduces a PR's checks to one state and the names of the failing ones. Any failure
// wins over pending, and pending wins over success. No checks at all is CINone.
func ciFromChecks(checks []checkJSON) (scm.CIState, []string) {
	if len(checks) == 0 {
		return scm.CINone, nil
	}
	var failing []string
	pending := false
	for _, c := range checks {
		if isStatusContext(c) {
			switch c.State {
			case "FAILURE", "ERROR":
				failing = append(failing, c.Context)
			case "PENDING", "EXPECTED":
				pending = true
			}
			continue
		}
		if failedConclusions[c.Conclusion] {
			failing = append(failing, c.Name)
		} else if c.Status != "COMPLETED" {
			pending = true
		}
	}
	switch {
	case len(failing) > 0:
		return scm.CIFailure, failing
	case pending:
		return scm.CIPending, nil
	}
	return scm.CISuccess, nil
}

// isStatusContext tells a StatusContext from a CheckRun, falling back to the fields present
// when gh omits __typename.
func isStatusContext(c checkJSON) bool {
	if c.Typename != "" {
		return c.Typename == "StatusContext"
	}
	return c.Context != ""
}

// FindPR returns the open pull request whose head is head, if there is one.
func (g *GitHub) FindPR(ctx context.Context, head string) (scm.PR, bool, error) {
	out, err := g.run(ctx, "", "pr", "list", "--repo", g.Repo, "--head", head,
		"--state", "open", "--json", "number", "--limit", "1")
	if err != nil {
		return scm.PR{}, false, fmt.Errorf("find PR for %s: %w", head, err)
	}
	var list []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return scm.PR{}, false, fmt.Errorf("decode PRs for %s: %w", head, err)
	}
	if len(list) == 0 {
		return scm.PR{}, false, nil
	}
	pr, err := g.GetPR(ctx, list[0].Number)
	if err != nil {
		return scm.PR{}, false, err
	}
	return pr, true, nil
}

// MergePR squash-merges a pull request and deletes its branch.
func (g *GitHub) MergePR(ctx context.Context, number int) error {
	if _, err := g.run(ctx, "", "pr", "merge", strconv.Itoa(number), "--repo", g.Repo,
		"--squash", "--delete-branch"); err != nil {
		return fmt.Errorf("merge PR #%d: %w", number, err)
	}
	return nil
}

// CILog returns the last maxBytes bytes of the logs of up to three failed workflow runs on the
// PR's branch. Each run's log is preceded by a "=== <name> ===" header.
func (g *GitHub) CILog(ctx context.Context, number int, maxBytes int) (string, error) {
	pr, err := g.GetPR(ctx, number)
	if err != nil {
		return "", err
	}
	out, err := g.run(ctx, "", "run", "list", "--repo", g.Repo, "--branch", pr.HeadBranch,
		"--json", "databaseId,conclusion,name", "--limit", "10")
	if err != nil {
		return "", fmt.Errorf("list runs for %s: %w", pr.HeadBranch, err)
	}
	var runs []struct {
		DatabaseID int64  `json:"databaseId"`
		Conclusion string `json:"conclusion"`
		Name       string `json:"name"`
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		return "", fmt.Errorf("decode runs for %s: %w", pr.HeadBranch, err)
	}

	var b strings.Builder
	fetched := 0
	for _, r := range runs {
		if strings.ToLower(r.Conclusion) != "failure" {
			continue
		}
		if fetched == maxCILogRuns {
			break
		}
		fetched++
		log, err := g.run(ctx, "", "run", "view", strconv.FormatInt(r.DatabaseID, 10),
			"--repo", g.Repo, "--log-failed")
		if err != nil {
			return "", fmt.Errorf("view log of run %d (%s): %w", r.DatabaseID, r.Name, err)
		}
		fmt.Fprintf(&b, "=== %s ===\n%s\n", r.Name, log)
	}
	return tail(b.String(), maxBytes), nil
}

// tail returns the last maxBytes bytes of s, moved forward to a UTF-8 boundary. A maxBytes of
// zero or less returns s unchanged.
func tail(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// issueJSON is the subset of `gh issue list --json` that foundry reads.
type issueJSON struct {
	Number    int        `json:"number"`
	URL       string     `json:"url"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	Labels    []nameJSON `json:"labels"`
	Author    *loginJSON `json:"author"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type loginJSON struct {
	Login string `json:"login"`
}

const issueFields = "number,url,title,body,state,labels,author,createdAt,updatedAt"

// ListIssues lists issues with the given label (all labels when empty) in state, which is
// passed to gh as-is ("open", "closed" or "all"; empty means open).
func (g *GitHub) ListIssues(ctx context.Context, label string, state string) ([]scm.Issue, error) {
	if state == "" {
		state = "open"
	}
	args := []string{"issue", "list", "--repo", g.Repo, "--state", state, "--limit", "100",
		"--json", issueFields}
	if label != "" {
		args = append(args, "--label", label)
	}
	out, err := g.run(ctx, "", args...)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return decodeIssues(out)
}

// SearchIssues runs a code-host search over this repository's issues, in any state.
func (g *GitHub) SearchIssues(ctx context.Context, query string) ([]scm.Issue, error) {
	out, err := g.run(ctx, "", "issue", "list", "--repo", g.Repo, "--state", "all",
		"--search", query, "--limit", "20", "--json", issueFields)
	if err != nil {
		return nil, fmt.Errorf("search issues %q: %w", query, err)
	}
	return decodeIssues(out)
}

func decodeIssues(out []byte) ([]scm.Issue, error) {
	var raw []issueJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode issues: %w", err)
	}
	issues := make([]scm.Issue, 0, len(raw))
	for _, r := range raw {
		is := scm.Issue{
			Number:    r.Number,
			URL:       r.URL,
			Title:     r.Title,
			Body:      r.Body,
			State:     strings.ToLower(r.State),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
		for _, l := range r.Labels {
			is.Labels = append(is.Labels, l.Name)
		}
		if r.Author != nil {
			is.Author = r.Author.Login
		}
		issues = append(issues, is)
	}
	return issues, nil
}

// CreateIssue creates an issue with its labels (created first if missing).
func (g *GitHub) CreateIssue(ctx context.Context, spec scm.IssueSpec) (scm.Issue, error) {
	if err := g.ensureLabels(ctx, spec.Labels); err != nil {
		return scm.Issue{}, err
	}
	args := []string{"issue", "create", "--repo", g.Repo, "--title", spec.Title, "--body-file", "-"}
	for _, l := range spec.Labels {
		args = append(args, "--label", l)
	}
	out, err := g.run(ctx, spec.Body, args...)
	if err != nil {
		return scm.Issue{}, fmt.Errorf("create issue %q: %w", spec.Title, err)
	}
	issueURL := lastLine(out)
	number, err := numberFromURL(issueURL)
	if err != nil {
		return scm.Issue{}, fmt.Errorf("create issue %q: %w", spec.Title, err)
	}
	return scm.Issue{
		Number: number,
		URL:    issueURL,
		Title:  spec.Title,
		Body:   spec.Body,
		State:  "open",
		Labels: append([]string(nil), spec.Labels...),
	}, nil
}

// CommentIssue posts body as a comment on an issue or pull request.
func (g *GitHub) CommentIssue(ctx context.Context, number int, body string) error {
	if _, err := g.run(ctx, body, "issue", "comment", strconv.Itoa(number), "--repo", g.Repo,
		"--body-file", "-"); err != nil {
		return fmt.Errorf("comment on #%d: %w", number, err)
	}
	return nil
}

// EditLabels adds and removes labels on an issue or pull request. Labels to add are created
// first if missing. With nothing to add or remove, no gh call is made.
func (g *GitHub) EditLabels(ctx context.Context, number int, add, remove []string) error {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	if err := g.ensureLabels(ctx, add); err != nil {
		return err
	}
	args := []string{"issue", "edit", strconv.Itoa(number), "--repo", g.Repo}
	if len(add) > 0 {
		args = append(args, "--add-label", strings.Join(add, ","))
	}
	if len(remove) > 0 {
		args = append(args, "--remove-label", strings.Join(remove, ","))
	}
	if _, err := g.run(ctx, "", args...); err != nil {
		return fmt.Errorf("edit labels on #%d: %w", number, err)
	}
	return nil
}

// ensureLabels creates each label in the repository unless this process already has. --force
// makes creation idempotent, and the color is derived from the name so it is stable.
func (g *GitHub) ensureLabels(ctx context.Context, names []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.labels == nil {
		g.labels = make(map[string]bool)
	}
	for _, name := range names {
		if g.labels[name] {
			continue
		}
		if _, err := g.run(ctx, "", "label", "create", name, "--repo", g.Repo, "--force",
			"--color", labelColor(name)); err != nil {
			return fmt.Errorf("ensure label %q: %w", name, err)
		}
		g.labels[name] = true
	}
	return nil
}

// labelColor returns a stable 6-digit hex color for a label name.
func labelColor(name string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return fmt.Sprintf("%06x", h.Sum64()&0xffffff)
}

// numberFromURL reads the trailing number from a PR or issue URL such as
// https://github.com/owner/name/pull/12.
func numberFromURL(raw string) (int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return 0, fmt.Errorf("no number in %q", raw)
	}
	n, err := strconv.Atoi(path.Base(u.Path))
	if err != nil {
		return 0, fmt.Errorf("no number in %q: %w", raw, err)
	}
	return n, nil
}

// lastLine returns the last non-empty line of out, which is where gh prints the created URL.
func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
