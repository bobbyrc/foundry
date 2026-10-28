// Package fake is an in-memory scm.SCM for engine tests. It keeps PRs and issues in maps, numbers
// them from one shared counter like GitHub does, and lets tests set CI results directly.
package fake

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/foundry/internal/scm"
)

// ErrNotFound is wrapped by errors for PR, issue or comment targets that do not exist.
var ErrNotFound = errors.New("not found")

// Fake is a scm.SCM that lives in memory. Its exported fields may be read and set by tests; all
// methods are safe for concurrent use.
type Fake struct {
	mu       sync.Mutex
	PRs      map[int]*scm.PR
	Issues   map[int]*scm.Issue
	Comments map[int][]string
	Merged   []int
	// PRBodies holds the latest body given to UpdatePRBody for each PR, and BodyUpdates counts
	// the calls per PR.
	PRBodies    map[int]string
	BodyUpdates map[int]int
	// DefaultCI is the CI state given to new PRs. Its zero value means scm.CISuccess.
	DefaultCI scm.CIState
	// HeadSHA, when set, gives a PR's head commit for a branch; it runs on OpenPR and GetPR.
	HeadSHA func(branch string) string
	// MergeErr, when set, is returned by MergePR and nothing merges.
	MergeErr error
	next     int
}

var _ scm.SCM = (*Fake)(nil)

// New returns an empty Fake.
func New() *Fake {
	return &Fake{
		PRs:      map[int]*scm.PR{},
		Issues:   map[int]*scm.Issue{},
		Comments: map[int][]string{},

		PRBodies:    map[int]string{},
		BodyUpdates: map[int]int{},
	}
}

// SetCI sets the CI state of PR number, with the names of its failing checks.
// It panics if the PR does not exist, since that is a bug in the test.
func (f *Fake) SetCI(number int, state scm.CIState, failing ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.PRs[number]
	if !ok {
		panic(fmt.Sprintf("fake: SetCI: no PR #%d", number))
	}
	pr.CI = state
	pr.FailingCI = append([]string(nil), failing...)
}

// AddIssue creates an open issue with the given title, body and labels, and returns it.
func (f *Fake) AddIssue(title, body string, labels ...string) scm.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addIssueLocked(title, body, labels)
}

func (f *Fake) addIssueLocked(title, body string, labels []string) scm.Issue {
	f.next++
	now := time.Now()
	is := &scm.Issue{
		Number:    f.next,
		URL:       fmt.Sprintf("https://fake.invalid/issues/%d", f.next),
		Title:     title,
		Body:      body,
		State:     "open",
		Labels:    append([]string(nil), labels...),
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.Issues[is.Number] = is
	return cloneIssue(*is)
}

// OpenPR records a new open PR for spec.Head. Its CI is DefaultCI and it is always mergeable.
func (f *Fake) OpenPR(_ context.Context, spec scm.PRSpec) (scm.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	pr := &scm.PR{
		Number:     f.next,
		URL:        fmt.Sprintf("https://fake.invalid/pull/%d", f.next),
		State:      "open",
		HeadBranch: spec.Head,
		Mergeable:  "mergeable",
		CI:         f.defaultCI(),
		Labels:     append([]string(nil), spec.Labels...),
	}
	f.refreshHeadLocked(pr)
	f.PRs[pr.Number] = pr
	return clonePR(*pr), nil
}

func (f *Fake) defaultCI() scm.CIState {
	if f.DefaultCI == "" {
		return scm.CISuccess
	}
	return f.DefaultCI
}

// refreshHeadLocked sets pr.HeadSHA from the HeadSHA hook, when there is one.
func (f *Fake) refreshHeadLocked(pr *scm.PR) {
	if f.HeadSHA != nil {
		pr.HeadSHA = f.HeadSHA(pr.HeadBranch)
	}
}

// GetPR returns a copy of PR number, or an error wrapping ErrNotFound.
func (f *Fake) GetPR(_ context.Context, number int) (scm.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.PRs[number]
	if !ok {
		return scm.PR{}, fmt.Errorf("fake: PR #%d: %w", number, ErrNotFound)
	}
	f.refreshHeadLocked(pr)
	return clonePR(*pr), nil
}

// FindPR returns the open PR whose head is head, lowest number first, or ok=false.
func (f *Fake) FindPR(_ context.Context, head string) (scm.PR, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found *scm.PR
	for _, pr := range f.PRs {
		if pr.State == "open" && pr.HeadBranch == head && (found == nil || pr.Number < found.Number) {
			found = pr
		}
	}
	if found == nil {
		return scm.PR{}, false, nil
	}
	f.refreshHeadLocked(found)
	return clonePR(*found), true, nil
}

// UpdatePRBody records body for PR number, or returns an error wrapping ErrNotFound.
func (f *Fake) UpdatePRBody(_ context.Context, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.PRs[number]; !ok {
		return fmt.Errorf("fake: update body of PR #%d: %w", number, ErrNotFound)
	}
	f.PRBodies[number] = body
	f.BodyUpdates[number]++
	return nil
}

// MergePR merges an open PR. It returns MergeErr when set, and refuses when headSHA is non-empty
// and is not the PR's current head. The merge commit is the PR's head
// SHA, or "fake-merge-<n>" when there is none.
func (f *Fake) MergePR(_ context.Context, number int, headSHA string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MergeErr != nil {
		return f.MergeErr
	}
	pr, ok := f.PRs[number]
	if !ok {
		return fmt.Errorf("fake: merge PR #%d: %w", number, ErrNotFound)
	}
	if pr.State != "open" {
		return fmt.Errorf("fake: merge PR #%d: state is %s, not open", number, pr.State)
	}
	f.refreshHeadLocked(pr)
	if headSHA != "" && headSHA != pr.HeadSHA {
		return fmt.Errorf("fake: merge PR #%d: head changed from %s to %s", number, headSHA, pr.HeadSHA)
	}
	pr.State = "merged"
	pr.MergeSHA = pr.HeadSHA
	if pr.MergeSHA == "" {
		pr.MergeSHA = fmt.Sprintf("fake-merge-%d", number)
	}
	f.Merged = append(f.Merged, number)
	return nil
}

// CILog returns a fixed placeholder log for PR number.
func (f *Fake) CILog(_ context.Context, number int, _ int) (string, error) {
	return fmt.Sprintf("fake CI log for #%d", number), nil
}

// ListIssues returns copies of issues with the label (any label when empty) in state, sorted by
// number. State "" or "open" means open issues; "all" matches every state.
func (f *Fake) ListIssues(_ context.Context, label string, state string) ([]scm.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state == "" {
		state = "open"
	}
	out := []scm.Issue{}
	for _, is := range f.sortedIssuesLocked() {
		if state != "all" && is.State != state {
			continue
		}
		if label != "" && !contains(is.Labels, label) {
			continue
		}
		out = append(out, cloneIssue(*is))
	}
	return out, nil
}

// SearchIssues returns copies of issues, in any state, whose title contains query
// (case-insensitively), sorted by number.
func (f *Fake) SearchIssues(_ context.Context, query string) ([]scm.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	needle := strings.ToLower(query)
	out := []scm.Issue{}
	for _, is := range f.sortedIssuesLocked() {
		if strings.Contains(strings.ToLower(is.Title), needle) {
			out = append(out, cloneIssue(*is))
		}
	}
	return out, nil
}

// CreateIssue records a new open issue and returns it.
func (f *Fake) CreateIssue(_ context.Context, spec scm.IssueSpec) (scm.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addIssueLocked(spec.Title, spec.Body, spec.Labels), nil
}

// CommentIssue appends body to the comments of an issue or PR. It returns an error wrapping
// ErrNotFound when neither exists.
func (f *Fake) CommentIssue(_ context.Context, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.existsLocked(number) {
		return fmt.Errorf("fake: comment on #%d: %w", number, ErrNotFound)
	}
	f.Comments[number] = append(f.Comments[number], body)
	return nil
}

// EditLabels adds the labels in add that are missing and removes those in remove, on an issue
// or a PR. It returns an error wrapping ErrNotFound when neither exists.
func (f *Fake) EditLabels(_ context.Context, number int, add, remove []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var labels *[]string
	if pr, ok := f.PRs[number]; ok {
		labels = &pr.Labels
	} else if is, ok := f.Issues[number]; ok {
		labels = &is.Labels
	} else {
		return fmt.Errorf("fake: edit labels on #%d: %w", number, ErrNotFound)
	}
	for _, l := range add {
		if !contains(*labels, l) {
			*labels = append(*labels, l)
		}
	}
	kept := []string{}
	for _, l := range *labels {
		if !contains(remove, l) {
			kept = append(kept, l)
		}
	}
	*labels = kept
	return nil
}

func (f *Fake) existsLocked(number int) bool {
	_, isPR := f.PRs[number]
	_, isIssue := f.Issues[number]
	return isPR || isIssue
}

// sortedIssuesLocked returns the issues ordered by number.
func (f *Fake) sortedIssuesLocked() []*scm.Issue {
	nums := make([]int, 0, len(f.Issues))
	for n := range f.Issues {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	out := make([]*scm.Issue, 0, len(nums))
	for _, n := range nums {
		out = append(out, f.Issues[n])
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func clonePR(pr scm.PR) scm.PR {
	pr.FailingCI = append([]string(nil), pr.FailingCI...)
	pr.Labels = append([]string(nil), pr.Labels...)
	return pr
}

func cloneIssue(is scm.Issue) scm.Issue {
	is.Labels = append([]string(nil), is.Labels...)
	return is
}
