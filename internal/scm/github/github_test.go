package github

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/scm"
)

const repo = "owner/name"

// prViewFields is the --json field list GetPR must request.
const prViewFields = "number,url,state,headRefName,headRefOid,mergeCommit,mergeable,labels,statusCheckRollup"

type call struct {
	args  []string
	stdin string
}

// fakeGH records every gh call and answers from canned output. A response is looked up by the
// longest of the first three arguments that matches a key ("run view 101", "run view", "pr").
type fakeGH struct {
	calls []call
	on    map[string]string
	errOn map[string]error
}

func newFake() *fakeGH {
	return &fakeGH{on: map[string]string{}, errOn: map[string]error{}}
}

func (f *fakeGH) runner() Runner {
	return func(_ context.Context, stdin string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, call{args: args, stdin: stdin})
		for n := min(3, len(args)); n >= 1; n-- {
			key := strings.Join(args[:n], " ")
			if err, ok := f.errOn[key]; ok {
				return nil, err
			}
			if out, ok := f.on[key]; ok {
				return []byte(out), nil
			}
		}
		return nil, nil
	}
}

// countCalls returns how many recorded calls start with the given words.
func (f *fakeGH) countCalls(prefix ...string) int {
	n := 0
	for _, c := range f.calls {
		if len(c.args) >= len(prefix) && equal(c.args[:len(prefix)], prefix) {
			n++
		}
	}
	return n
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOpenPRCreatesLabelsAndReturnsPR(t *testing.T) {
	f := newFake()
	f.on["pr create"] = "Creating pull request...\nhttps://github.com/owner/name/pull/12\n"
	f.on["pr view"] = `{"number":12,"url":"https://github.com/owner/name/pull/12","state":"OPEN",
		"headRefName":"foundry/x","headRefOid":"abc","mergeCommit":null,"mergeable":"MERGEABLE",
		"labels":[{"name":"foundry"},{"name":"bug"}],"statusCheckRollup":[]}`
	g := NewWithRunner(repo, f.runner())

	pr, err := g.OpenPR(context.Background(), scm.PRSpec{
		Head: "foundry/x", Base: "main", Title: "Fix it", Body: "the body",
		Labels: []string{"foundry", "bug"}, Draft: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 12, pr.Number)
	assert.Equal(t, "open", pr.State)
	assert.Equal(t, "foundry/x", pr.HeadBranch)
	assert.Equal(t, []string{"foundry", "bug"}, pr.Labels)
	assert.Equal(t, scm.CINone, pr.CI)

	require.Len(t, f.calls, 5)
	assert.Equal(t, []string{"pr", "create", "--repo", repo, "--head", "foundry/x", "--base", "main",
		"--title", "Fix it", "--body-file", "-", "--draft"}, f.calls[0].args)
	assert.Equal(t, "the body", f.calls[0].stdin)
	assert.Equal(t, []string{"label", "create", "foundry", "--repo", repo, "--force", "--color"},
		f.calls[1].args[:7])
	assert.Equal(t, []string{"label", "create", "bug", "--repo", repo, "--force", "--color"},
		f.calls[2].args[:7])
	assert.Equal(t, []string{"pr", "edit", "12", "--repo", repo, "--add-label", "foundry,bug"},
		f.calls[3].args)
	assert.Equal(t, []string{"pr", "view", "12", "--repo", repo, "--json", prViewFields}, f.calls[4].args)
}

func TestOpenPRWithoutDraftOrLabels(t *testing.T) {
	f := newFake()
	f.on["pr create"] = "https://github.com/owner/name/pull/3"
	f.on["pr view"] = `{"number":3,"state":"OPEN","headRefName":"b","statusCheckRollup":[]}`
	g := NewWithRunner(repo, f.runner())

	_, err := g.OpenPR(context.Background(), scm.PRSpec{Head: "b", Base: "main", Title: "t"})
	require.NoError(t, err)
	require.Len(t, f.calls, 2)
	assert.Equal(t, []string{"pr", "create", "--repo", repo, "--head", "b", "--base", "main",
		"--title", "t", "--body-file", "-"}, f.calls[0].args)
}

func TestOpenPRFailsOnURLWithoutNumber(t *testing.T) {
	f := newFake()
	f.on["pr create"] = "something unexpected"
	g := NewWithRunner(repo, f.runner())
	_, err := g.OpenPR(context.Background(), scm.PRSpec{Head: "b", Base: "main"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something unexpected")
}

func TestGetPRMapsStatesAndMergeability(t *testing.T) {
	cases := []struct {
		state, mergeable, wantState, wantMergeable string
		mergeCommit                                string
		wantMergeSHA                               string
	}{
		{"MERGED", "UNKNOWN", "merged", "unknown", `{"oid":"deadbeef"}`, "deadbeef"},
		{"CLOSED", "CONFLICTING", "closed", "conflicting", `null`, ""},
		{"OPEN", "MERGEABLE", "open", "mergeable", `null`, ""},
	}
	for _, c := range cases {
		f := newFake()
		f.on["pr view"] = `{"number":5,"state":"` + c.state + `","headRefName":"h","headRefOid":"sha",` +
			`"mergeCommit":` + c.mergeCommit + `,"mergeable":"` + c.mergeable + `","labels":[],` +
			`"statusCheckRollup":[]}`
		g := NewWithRunner(repo, f.runner())
		pr, err := g.GetPR(context.Background(), 5)
		require.NoError(t, err)
		assert.Equal(t, c.wantState, pr.State)
		assert.Equal(t, c.wantMergeable, pr.Mergeable)
		assert.Equal(t, c.wantMergeSHA, pr.MergeSHA)
		assert.Equal(t, "sha", pr.HeadSHA)
		assert.Equal(t, []string{"pr", "view", "5", "--repo", repo, "--json", prViewFields}, f.calls[0].args)
	}
}

func TestCIFromChecks(t *testing.T) {
	cr := func(status, conclusion, name string) checkJSON {
		return checkJSON{Typename: "CheckRun", Name: name, Status: status, Conclusion: conclusion}
	}
	sc := func(state, context string) checkJSON {
		return checkJSON{Typename: "StatusContext", State: state, Context: context}
	}
	cases := []struct {
		name        string
		checks      []checkJSON
		wantState   scm.CIState
		wantFailing []string
	}{
		{"none", nil, scm.CINone, nil},
		{"check in progress", []checkJSON{cr("IN_PROGRESS", "", "test")}, scm.CIPending, nil},
		{"check queued", []checkJSON{cr("QUEUED", "", "test")}, scm.CIPending, nil},
		{"status pending", []checkJSON{sc("PENDING", "vercel")}, scm.CIPending, nil},
		{"status expected", []checkJSON{sc("EXPECTED", "deploy")}, scm.CIPending, nil},
		{"success", []checkJSON{cr("COMPLETED", "SUCCESS", "a")}, scm.CISuccess, nil},
		{"neutral and skipped pass", []checkJSON{
			cr("COMPLETED", "NEUTRAL", "a"), cr("COMPLETED", "SKIPPED", "b")}, scm.CISuccess, nil},
		{"status success", []checkJSON{sc("SUCCESS", "vercel")}, scm.CISuccess, nil},
		{"check failure", []checkJSON{cr("COMPLETED", "FAILURE", "lint")}, scm.CIFailure, []string{"lint"}},
		{"timed out", []checkJSON{cr("COMPLETED", "TIMED_OUT", "e2e")}, scm.CIFailure, []string{"e2e"}},
		{"cancelled", []checkJSON{cr("COMPLETED", "CANCELLED", "build")}, scm.CIFailure, []string{"build"}},
		{"action required", []checkJSON{cr("COMPLETED", "ACTION_REQUIRED", "approve")}, scm.CIFailure, []string{"approve"}},
		{"startup failure", []checkJSON{cr("COMPLETED", "STARTUP_FAILURE", "boot")}, scm.CIFailure, []string{"boot"}},
		{"status failure", []checkJSON{sc("FAILURE", "ci/x")}, scm.CIFailure, []string{"ci/x"}},
		{"status error", []checkJSON{sc("ERROR", "ci/y")}, scm.CIFailure, []string{"ci/y"}},
		{"mixed success and pending", []checkJSON{
			cr("COMPLETED", "SUCCESS", "a"), sc("PENDING", "vercel")}, scm.CIPending, nil},
		{"failure beats pending", []checkJSON{
			sc("FAILURE", "ci/x"), cr("IN_PROGRESS", "", "test")}, scm.CIFailure, []string{"ci/x"}},
		{"several failures", []checkJSON{
			cr("COMPLETED", "FAILURE", "lint"), cr("COMPLETED", "SUCCESS", "a"),
			sc("ERROR", "ci/y")}, scm.CIFailure, []string{"lint", "ci/y"}},
		{"typename missing falls back to fields", []checkJSON{{Context: "ci/z", State: "FAILURE"}},
			scm.CIFailure, []string{"ci/z"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state, failing := ciFromChecks(c.checks)
			assert.Equal(t, c.wantState, state)
			assert.Equal(t, c.wantFailing, failing)
		})
	}
}

func TestGetPRCarriesCIAndFailingNames(t *testing.T) {
	f := newFake()
	f.on["pr view"] = `{"number":9,"state":"OPEN","headRefName":"h","mergeable":"MERGEABLE",
		"statusCheckRollup":[
			{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"},
			{"__typename":"StatusContext","context":"vercel","state":"PENDING"}]}`
	g := NewWithRunner(repo, f.runner())
	pr, err := g.GetPR(context.Background(), 9)
	require.NoError(t, err)
	assert.Equal(t, scm.CIFailure, pr.CI)
	assert.Equal(t, []string{"lint"}, pr.FailingCI)
}

func TestFindPR(t *testing.T) {
	t.Run("none open", func(t *testing.T) {
		f := newFake()
		f.on["pr list"] = `[]`
		g := NewWithRunner(repo, f.runner())
		pr, ok, err := g.FindPR(context.Background(), "foundry/x")
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Equal(t, scm.PR{}, pr)
		require.Len(t, f.calls, 1)
		assert.Equal(t, []string{"pr", "list", "--repo", repo, "--head", "foundry/x",
			"--state", "open", "--json", "number", "--limit", "1"}, f.calls[0].args)
	})
	t.Run("found", func(t *testing.T) {
		f := newFake()
		f.on["pr list"] = `[{"number":7}]`
		f.on["pr view"] = `{"number":7,"state":"OPEN","headRefName":"foundry/x","statusCheckRollup":[]}`
		g := NewWithRunner(repo, f.runner())
		pr, ok, err := g.FindPR(context.Background(), "foundry/x")
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, 7, pr.Number)
		assert.Equal(t, []string{"pr", "view", "7", "--repo", repo, "--json", prViewFields}, f.calls[1].args)
	})
}

func TestMergePR(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	require.NoError(t, g.MergePR(context.Background(), 4, ""))
	require.Len(t, f.calls, 1)
	assert.Equal(t, []string{"pr", "merge", "4", "--repo", repo, "--squash", "--delete-branch"}, f.calls[0].args)

	f.errOn["pr merge"] = errors.New("exit status 1: not mergeable")
	err := g.MergePR(context.Background(), 4, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge PR #4")
	assert.Contains(t, err.Error(), "not mergeable")
}

func TestMergePRMatchesHeadCommit(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	require.NoError(t, g.MergePR(context.Background(), 4, "abc123"))
	require.Len(t, f.calls, 1)
	assert.Equal(t, []string{"pr", "merge", "4", "--repo", repo, "--squash", "--delete-branch",
		"--match-head-commit", "abc123"}, f.calls[0].args)
}

func TestCILogFetchesFailedRunsAndTails(t *testing.T) {
	f := newFake()
	f.on["pr view"] = `{"number":2,"state":"OPEN","headRefName":"foundry/x","statusCheckRollup":[]}`
	f.on["run list"] = `[
		{"databaseId":101,"conclusion":"failure","name":"ci"},
		{"databaseId":102,"conclusion":"success","name":"docs"},
		{"databaseId":103,"conclusion":"failure","name":"e2e"},
		{"databaseId":104,"conclusion":"failure","name":"lint"},
		{"databaseId":105,"conclusion":"failure","name":"extra"}]`
	f.on["run view 101"] = "ci log line"
	f.on["run view 103"] = "e2e log line"
	f.on["run view 104"] = "lint log line"
	f.on["run view 105"] = "must not be fetched"
	g := NewWithRunner(repo, f.runner())

	log, err := g.CILog(context.Background(), 2, 10000)
	require.NoError(t, err)
	assert.Equal(t, []string{"pr", "view", "2", "--repo", repo, "--json", prViewFields}, f.calls[0].args)
	assert.Equal(t, []string{"run", "list", "--repo", repo, "--branch", "foundry/x",
		"--json", "databaseId,conclusion,name", "--limit", "10"}, f.calls[1].args)
	assert.Equal(t, 3, f.countCalls("run", "view"))
	assert.Equal(t, []string{"run", "view", "101", "--repo", repo, "--log-failed"}, f.calls[2].args)
	assert.Equal(t, "=== ci ===\nci log line\n=== e2e ===\ne2e log line\n=== lint ===\nlint log line\n", log)

	// A tail keeps only the last maxBytes bytes.
	tailed, err := g.CILog(context.Background(), 2, 10)
	require.NoError(t, err)
	assert.Len(t, tailed, 10)
	assert.True(t, strings.HasSuffix(log, tailed))
}

const sampleFailedLog = "Go vet, format, test\tSet up job\t2026-10-28T04:08:10.1000000Z \x1b[36;1mgo version\x1b[0m\n" +
	"Go vet, format, test\tgo test\t2026-10-28T04:08:18.5700000Z ok  \tfoundry/internal/a\t0.1s\n" +
	"Go vet, format, test\tgo test\t2026-10-28T04:08:18.5800911Z --- FAIL: TestExecTimeout (5.01s)\n" +
	"Go vet, format, test\tgo test\t2026-10-28T04:08:18.5900000Z \x1b[31;1mFAIL\x1b[0m\n" +
	"Go vet, format, test\tUpload\t2026-10-28T04:08:19.0000000Z ##[error]Process completed with exit code 1.\n"

func TestCleanLog(t *testing.T) {
	assert.Equal(t, "=== Go vet, format, test / Set up job ===\ngo version\n"+
		"=== Go vet, format, test / go test ===\nok  \tfoundry/internal/a\t0.1s\n"+
		"--- FAIL: TestExecTimeout (5.01s)\nFAIL\n"+
		"=== Go vet, format, test / Upload ===\n##[error]Process completed with exit code 1.\n",
		cleanLog(sampleFailedLog))
	assert.Equal(t, "plain line\n", cleanLog("plain line"))
}

func TestCondenseLogKeepsContextAroundFailures(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("line %03d", i))
	}
	lines[60] = "--- FAIL: TestX"
	s := strings.Join(lines, "\n") + "\n"

	assert.Equal(t, s, condenseLog(s, 0))
	assert.Equal(t, s, condenseLog(s, len(s)))

	out := condenseLog(s, len(s)-1)
	assert.True(t, strings.HasPrefix(out, "[... 40 lines omitted ...]\nline 040\n"), out)
	assert.True(t, strings.HasSuffix(out, "line 059\n--- FAIL: TestX\n[... 39 lines omitted ...]\n"), out)

	// Still too long after condensing: fall back to the tail.
	assert.Len(t, condenseLog(s, 30), 30)
}

func TestCILogCleansAndCondensesRealLines(t *testing.T) {
	f := newFake()
	f.on["pr view"] = `{"number":2,"state":"OPEN","headRefName":"foundry/x","statusCheckRollup":[]}`
	f.on["run list"] = `[{"databaseId":101,"conclusion":"failure","name":"ci"}]`
	f.on["run view 101"] = sampleFailedLog
	g := NewWithRunner(repo, f.runner())

	log, err := g.CILog(context.Background(), 2, 100000)
	require.NoError(t, err)
	assert.NotContains(t, log, "\x1b")
	assert.NotContains(t, log, "2026-10-28T")
	assert.Contains(t, log, "=== ci ===\n=== Go vet, format, test / Set up job ===\ngo version\n")

	small, err := g.CILog(context.Background(), 2, len(log)-1)
	require.NoError(t, err)
	assert.Contains(t, small, "--- FAIL: TestExecTimeout")
}

func TestTailKeepsUTF8Boundary(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	out := tail(s, 5)
	assert.True(t, utf8.ValidString(out))
	assert.Equal(t, strings.Repeat("é", 2), out)
	assert.Equal(t, s, tail(s, 0))
	assert.Equal(t, s, tail(s, 1000))
}

func TestListIssues(t *testing.T) {
	f := newFake()
	f.on["issue list"] = `[{"number":3,"url":"https://github.com/owner/name/issues/3","title":"Flaky",
		"body":"b","state":"OPEN","labels":[{"name":"friction"}],"author":{"login":"bob"},
		"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-02T10:00:00Z"}]`
	g := NewWithRunner(repo, f.runner())

	issues, err := g.ListIssues(context.Background(), "friction", "open")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, scm.Issue{
		Number:    3,
		URL:       "https://github.com/owner/name/issues/3",
		Title:     "Flaky",
		Body:      "b",
		State:     "open",
		Labels:    []string{"friction"},
		Author:    "bob",
		CreatedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC),
	}, issues[0])
	assert.Equal(t, []string{"issue", "list", "--repo", repo, "--state", "open", "--limit", "100",
		"--json", "number,url,title,body,state,labels,author,createdAt,updatedAt",
		"--label", "friction"}, f.calls[0].args)

	_, err = g.ListIssues(context.Background(), "", "closed")
	require.NoError(t, err)
	assert.NotContains(t, f.calls[1].args, "--label")
	assert.Equal(t, "closed", f.calls[1].args[5])
}

func TestSearchIssues(t *testing.T) {
	f := newFake()
	f.on["issue list"] = `[]`
	g := NewWithRunner(repo, f.runner())
	issues, err := g.SearchIssues(context.Background(), "flaky test")
	require.NoError(t, err)
	assert.NotNil(t, issues)
	assert.Empty(t, issues)
	assert.Equal(t, []string{"issue", "list", "--repo", repo, "--state", "all", "--search", "flaky test",
		"--limit", "20", "--json", "number,url,title,body,state,labels,author,createdAt,updatedAt"},
		f.calls[0].args)
}

func TestCreateIssueEnsuresLabelsOnce(t *testing.T) {
	f := newFake()
	f.on["issue create"] = "https://github.com/owner/name/issues/21\n"
	g := NewWithRunner(repo, f.runner())

	issue, err := g.CreateIssue(context.Background(), scm.IssueSpec{
		Title: "Friction: slow tests", Body: "details", Labels: []string{"friction", "foundry"},
	})
	require.NoError(t, err)
	assert.Equal(t, scm.Issue{
		Number: 21, URL: "https://github.com/owner/name/issues/21", Title: "Friction: slow tests",
		Body: "details", State: "open", Labels: []string{"friction", "foundry"},
	}, issue)

	createIdx := -1
	for i, c := range f.calls {
		if c.args[0] == "issue" && c.args[1] == "create" {
			createIdx = i
		}
	}
	require.NotEqual(t, -1, createIdx)
	assert.Equal(t, []string{"issue", "create", "--repo", repo, "--title", "Friction: slow tests",
		"--body-file", "-", "--label", "friction", "--label", "foundry"}, f.calls[createIdx].args)
	assert.Equal(t, "details", f.calls[createIdx].stdin)

	// Labels are created once per process, so a later edit with the same label does not
	// create it again.
	before := f.countCalls("label", "create")
	require.NoError(t, g.EditLabels(context.Background(), 21, []string{"friction"}, nil))
	assert.Equal(t, before, f.countCalls("label", "create"))
	assert.Equal(t, 2, before)
}

func TestCommentIssue(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	require.NoError(t, g.CommentIssue(context.Background(), 8, "hello there"))
	require.Len(t, f.calls, 1)
	assert.Equal(t, []string{"issue", "comment", "8", "--repo", repo, "--body-file", "-"}, f.calls[0].args)
	assert.Equal(t, "hello there", f.calls[0].stdin)
}

func TestEditLabelsAddAndRemove(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	require.NoError(t, g.EditLabels(context.Background(), 6, []string{"approved"}, []string{"blocked", "wip"}))
	assert.Equal(t, []string{"issue", "edit", "6", "--repo", repo, "--add-label", "approved",
		"--remove-label", "blocked,wip"}, f.calls[len(f.calls)-1].args)

	f.calls = nil
	require.NoError(t, g.EditLabels(context.Background(), 6, nil, []string{"blocked"}))
	require.Len(t, f.calls, 1)
	assert.Equal(t, []string{"issue", "edit", "6", "--repo", repo, "--remove-label", "blocked"}, f.calls[0].args)

	f.calls = nil
	require.NoError(t, g.EditLabels(context.Background(), 6, nil, nil))
	assert.Empty(t, f.calls)
}

func TestLabelColorIsStableHex(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{6}$`)
	assert.Regexp(t, re, labelColor("foundry"))
	assert.Equal(t, labelColor("friction"), labelColor("friction"))
	assert.NotEqual(t, labelColor("friction"), labelColor("foundry"))
}

func TestNumberFromURL(t *testing.T) {
	n, err := numberFromURL("https://github.com/owner/name/pull/12")
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	_, err = numberFromURL("https://github.com/owner/name/pull/abc")
	assert.Error(t, err)
	_, err = numberFromURL("")
	assert.Error(t, err)
}

func TestErrorsIncludeGHStderr(t *testing.T) {
	f := newFake()
	f.errOn["issue comment"] = errors.New("gh: HTTP 404: Not Found")
	g := NewWithRunner(repo, f.runner())
	err := g.CommentIssue(context.Background(), 1, "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "comment on #1")
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestLabelFailureIsNotCached(t *testing.T) {
	f := newFake()
	f.errOn["label create"] = errors.New("network down")
	g := NewWithRunner(repo, f.runner())
	err := g.EditLabels(context.Background(), 1, []string{"x"}, nil)
	require.Error(t, err)

	delete(f.errOn, "label create")
	require.NoError(t, g.EditLabels(context.Background(), 1, []string{"x"}, nil))
	assert.Equal(t, 2, f.countCalls("label", "create"))
}

func TestUpdatePRBodySendsBodyOnStdin(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	body := "line one\n`$(rm -rf /)` \"quoted\"\n"

	require.NoError(t, g.UpdatePRBody(context.Background(), 12, body))
	require.Len(t, f.calls, 1)
	assert.Equal(t, []string{"pr", "edit", "12", "--repo", repo, "--body-file", "-"}, f.calls[0].args)
	assert.Equal(t, body, f.calls[0].stdin)
}

func TestUpdatePRBodyWrapsError(t *testing.T) {
	f := newFake()
	f.errOn["pr edit"] = errors.New("boom")
	g := NewWithRunner(repo, f.runner())

	err := g.UpdatePRBody(context.Background(), 12, "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update body of PR #12")
	assert.ErrorContains(t, err, "boom")
}
