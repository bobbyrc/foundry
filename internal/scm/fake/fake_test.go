package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/scm"
)

var ctx = context.Background()

func TestOpenPRDefaultsAndSharedNumbering(t *testing.T) {
	f := New()
	issue := f.AddIssue("first", "body", "foundry")
	assert.Equal(t, 1, issue.Number)

	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "foundry/a", Base: "main", Title: "t", Labels: []string{"bug"}})
	require.NoError(t, err)
	assert.Equal(t, 2, pr.Number, "PRs and issues share one numbering")
	assert.Equal(t, "open", pr.State)
	assert.Equal(t, "foundry/a", pr.HeadBranch)
	assert.Equal(t, "mergeable", pr.Mergeable)
	assert.Equal(t, scm.CISuccess, pr.CI, "zero DefaultCI means success")
	assert.Equal(t, []string{"bug"}, pr.Labels)
}

func TestDefaultCIIsUsedForNewPRs(t *testing.T) {
	f := New()
	f.DefaultCI = scm.CIPending
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)
	assert.Equal(t, scm.CIPending, pr.CI)
}

func TestGetPRReturnsCopyAndNotFound(t *testing.T) {
	f := New()
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h", Labels: []string{"x"}})
	require.NoError(t, err)

	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	got.Labels[0] = "mutated"
	got.State = "closed"
	again, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, []string{"x"}, again.Labels)
	assert.Equal(t, "open", again.State)

	_, err = f.GetPR(ctx, 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.Contains(t, err.Error(), "not found")
}

func TestHeadSHAHookRefreshesOnOpenAndGet(t *testing.T) {
	f := New()
	head := "sha1"
	f.HeadSHA = func(branch string) string { return branch + "@" + head }

	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "b"})
	require.NoError(t, err)
	assert.Equal(t, "b@sha1", pr.HeadSHA)

	head = "sha2"
	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, "b@sha2", got.HeadSHA)
}

func TestSetCI(t *testing.T) {
	f := New()
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)

	f.SetCI(pr.Number, scm.CIFailure, "lint", "e2e")
	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, scm.CIFailure, got.CI)
	assert.Equal(t, []string{"lint", "e2e"}, got.FailingCI)

	f.SetCI(pr.Number, scm.CISuccess)
	got, err = f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, scm.CISuccess, got.CI)
	assert.Empty(t, got.FailingCI)

	assert.Panics(t, func() { f.SetCI(404, scm.CIPending) })
}

func TestFindPRByHeadAmongOpenPRs(t *testing.T) {
	f := New()
	_, ok, err := f.FindPR(ctx, "h")
	require.NoError(t, err)
	assert.False(t, ok)

	first, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)
	second, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)
	require.NoError(t, f.MergePR(ctx, first.Number, ""))

	pr, ok, err := f.FindPR(ctx, "h")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, second.Number, pr.Number, "merged PRs are not open")

	require.NoError(t, f.MergePR(ctx, second.Number, ""))
	_, ok, err = f.FindPR(ctx, "h")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMergePR(t *testing.T) {
	f := New()
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)

	require.NoError(t, f.MergePR(ctx, pr.Number, ""))
	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, "merged", got.State)
	assert.Equal(t, "fake-merge-1", got.MergeSHA)
	assert.Equal(t, []int{pr.Number}, f.Merged)

	// Merging again is refused and is not recorded twice.
	assert.Error(t, f.MergePR(ctx, pr.Number, ""))
	assert.Equal(t, []int{pr.Number}, f.Merged)

	assert.True(t, errors.Is(f.MergePR(ctx, 77, ""), ErrNotFound))
}

func TestMergePRRefusesChangedHead(t *testing.T) {
	f := New()
	head := "aaa"
	f.HeadSHA = func(string) string { return head }
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)

	head = "bbb" // a new push after CI and review approved "aaa"
	err = f.MergePR(ctx, pr.Number, "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "head changed")
	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, "open", got.State)
	assert.Empty(t, f.Merged)

	require.NoError(t, f.MergePR(ctx, pr.Number, "bbb"))
	got, err = f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, "merged", got.State)
}

func TestMergePRUsesHeadSHAAndMergeErr(t *testing.T) {
	f := New()
	f.HeadSHA = func(string) string { return "cafe" }
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)
	require.NoError(t, f.MergePR(ctx, pr.Number, ""))
	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, "cafe", got.MergeSHA)

	f2 := New()
	f2.MergeErr = errors.New("branch protection")
	pr2, err := f2.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)
	err = f2.MergePR(ctx, pr2.Number, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch protection")
	assert.Empty(t, f2.Merged)
	got2, err := f2.GetPR(ctx, pr2.Number)
	require.NoError(t, err)
	assert.Equal(t, "open", got2.State)
}

func TestCILog(t *testing.T) {
	f := New()
	log, err := f.CILog(ctx, 12, 1000)
	require.NoError(t, err)
	assert.Equal(t, "fake CI log for #12", log)
}

func TestListIssuesFiltersAndSorts(t *testing.T) {
	f := New()
	a := f.AddIssue("a", "", "foundry")
	b := f.AddIssue("b", "", "other")
	c := f.AddIssue("c", "", "foundry")
	require.NoError(t, f.EditLabels(ctx, c.Number, nil, nil))
	_, err := f.CreateIssue(ctx, scm.IssueSpec{Title: "closed one"})
	require.NoError(t, err)
	f.Issues[a.Number].State = "closed"

	open, err := f.ListIssues(ctx, "", "open")
	require.NoError(t, err)
	assert.Equal(t, []int{b.Number, c.Number, 4}, numbers(open))

	labelled, err := f.ListIssues(ctx, "foundry", "all")
	require.NoError(t, err)
	assert.Equal(t, []int{a.Number, c.Number}, numbers(labelled))

	closed, err := f.ListIssues(ctx, "", "closed")
	require.NoError(t, err)
	assert.Equal(t, []int{a.Number}, numbers(closed))

	none, err := f.ListIssues(ctx, "missing", "all")
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestSearchIssuesCaseInsensitiveTitle(t *testing.T) {
	f := New()
	f.AddIssue("Flaky Test in CI", "body")
	f.AddIssue("unrelated", "flaky body does not count")
	closed := f.AddIssue("FLAKY closed", "")
	f.Issues[closed.Number].State = "closed"

	got, err := f.SearchIssues(ctx, "flaky")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 3}, numbers(got))

	none, err := f.SearchIssues(ctx, "nothing matches")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestCreateIssue(t *testing.T) {
	f := New()
	issue, err := f.CreateIssue(ctx, scm.IssueSpec{Title: "New", Body: "b", Labels: []string{"friction"}})
	require.NoError(t, err)
	assert.Equal(t, "open", issue.State)
	assert.Equal(t, "New", issue.Title)
	assert.Equal(t, []string{"friction"}, issue.Labels)
	assert.Equal(t, issue.Title, f.Issues[issue.Number].Title)
}

func TestCommentIssueOnIssueAndPR(t *testing.T) {
	f := New()
	issue := f.AddIssue("i", "")
	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h"})
	require.NoError(t, err)

	require.NoError(t, f.CommentIssue(ctx, issue.Number, "one"))
	require.NoError(t, f.CommentIssue(ctx, issue.Number, "two"))
	require.NoError(t, f.CommentIssue(ctx, pr.Number, "on pr"))
	assert.Equal(t, []string{"one", "two"}, f.Comments[issue.Number])
	assert.Equal(t, []string{"on pr"}, f.Comments[pr.Number])

	err = f.CommentIssue(ctx, 500, "nobody")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestEditLabelsOnIssueAndPR(t *testing.T) {
	f := New()
	issue := f.AddIssue("i", "", "a", "b")
	require.NoError(t, f.EditLabels(ctx, issue.Number, []string{"c", "a"}, []string{"b"}))
	assert.Equal(t, []string{"a", "c"}, f.Issues[issue.Number].Labels)

	pr, err := f.OpenPR(ctx, scm.PRSpec{Head: "h", Labels: []string{"x"}})
	require.NoError(t, err)
	require.NoError(t, f.EditLabels(ctx, pr.Number, []string{"y"}, []string{"x"}))
	got, err := f.GetPR(ctx, pr.Number)
	require.NoError(t, err)
	assert.Equal(t, []string{"y"}, got.Labels)

	err = f.EditLabels(ctx, 999, []string{"z"}, nil)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestAddIssueReturnsCopy(t *testing.T) {
	f := New()
	issue := f.AddIssue("t", "b", "l1")
	issue.Labels[0] = "mutated"
	assert.Equal(t, []string{"l1"}, f.Issues[issue.Number].Labels)
}

func numbers(issues []scm.Issue) []int {
	out := make([]int, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.Number)
	}
	return out
}
