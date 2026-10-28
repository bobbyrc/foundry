// Package scm talks to the code host (pull requests, issues, CI status). scm/github implements
// it with the gh CLI.
package scm

import (
	"context"
	"time"
)

type PRSpec struct {
	Head   string // branch name
	Base   string
	Title  string
	Body   string
	Labels []string
	Draft  bool
}

type CIState string

const (
	CINone    CIState = "none" // no checks configured
	CIPending CIState = "pending"
	CISuccess CIState = "success"
	CIFailure CIState = "failure"
)

type PR struct {
	Number     int
	URL        string
	State      string // "open", "closed", "merged"
	HeadBranch string
	HeadSHA    string
	MergeSHA   string
	Mergeable  string // "mergeable", "conflicting", "unknown"
	CI         CIState
	FailingCI  []string // names of failing checks
	Labels     []string
}

type Issue struct {
	Number    int
	URL       string
	Title     string
	Body      string
	State     string // "open", "closed"
	Labels    []string
	Author    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Comments  []IssueComment
}

// IssueComment is one comment on an issue. Association is GitHub's authorAssociation as-is
// (OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, NONE, ...).
type IssueComment struct {
	Author      string
	Association string
	Body        string
	CreatedAt   time.Time
}

type IssueSpec struct {
	Title  string
	Body   string
	Labels []string
}

// SCM is scoped to one repository ("owner/name").
type SCM interface {
	OpenPR(ctx context.Context, spec PRSpec) (PR, error)
	GetPR(ctx context.Context, number int) (PR, error)
	// FindPR returns the open PR for head branch, or ok=false.
	FindPR(ctx context.Context, head string) (pr PR, ok bool, err error)
	// UpdatePRBody replaces the description of PR number.
	UpdatePRBody(ctx context.Context, number int, body string) error
	// MergePR squash-merges and deletes the branch. When headSHA is non-empty the merge only
	// happens if the PR head is still that commit (the one CI, review and policy approved).
	MergePR(ctx context.Context, number int, headSHA string) error
	// CILog returns a tail of the failing checks' logs, for the agent fixing them.
	CILog(ctx context.Context, number int, maxBytes int) (string, error)

	ListIssues(ctx context.Context, label string, state string) ([]Issue, error)
	// SearchIssues runs a code-host search scoped to this repo (used to dedupe friction).
	SearchIssues(ctx context.Context, query string) ([]Issue, error)
	CreateIssue(ctx context.Context, spec IssueSpec) (Issue, error)
	CommentIssue(ctx context.Context, number int, body string) error
	// CloseIssue closes an issue as completed with a comment (no-op if already closed).
	CloseIssue(ctx context.Context, number int, comment string) error
	EditLabels(ctx context.Context, number int, add, remove []string) error
}
