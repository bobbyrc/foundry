package engine

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/domain"
)

//go:embed prompts/*.md
var promptFS embed.FS

// BuiltinPrompt returns the embedded prompts/<name>.md, or an error for an unknown name.
func (e *Engine) BuiltinPrompt(name string) (string, error) {
	b, err := promptFS.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("unknown builtin prompt %q", name)
	}
	return string(b), nil
}

// loadPrompt returns a role prompt: "builtin:<name>" from the embedded prompts directory, or
// the contents of a Markdown file path (read on every call, so edits apply without restart).
func (e *Engine) loadPrompt(ref string) (string, error) {
	if name, ok := strings.CutPrefix(ref, "builtin:"); ok {
		return e.BuiltinPrompt(name)
	}
	b, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("read prompt %s: %w", ref, err)
	}
	return string(b), nil
}

// AgentPrompt is the agent's own instructions: PromptText, else the builtin or file Prompt.
func (e *Engine) AgentPrompt(a config.Agent) (string, error) {
	if a.PromptText != "" {
		return a.PromptText, nil
	}
	return e.loadPrompt(a.Prompt)
}

// commonPrompt returns prompts/common.md, the rules and report contract every worker gets.
func (e *Engine) commonPrompt() string {
	b, _ := promptFS.ReadFile("prompts/common.md") // embedded at build time; cannot be missing
	return string(b)
}

// buildInstructions is the system-prompt text for an agent on a project: its role prompt, the
// common prompt, then the project's own instructions.
func (e *Engine) buildInstructions(agent config.Agent, p *config.Project) (string, error) {
	role, err := e.AgentPrompt(agent)
	if err != nil {
		return "", err
	}
	out := role + "\n\n" + e.commonPrompt()
	if p != nil && p.Instructions != "" {
		out += "\n\n## Project instructions\n\n" + p.Instructions
	}
	return out, nil
}

// buildTaskPrompt is the user turn for a run: the task's facts and body. For a review task it
// names the pull request under review and includes the task the change was meant to
// accomplish (the parent's body). conflicts lists the files that conflicted when onlet merged
// the default branch into the task's branch; the agent must resolve them.
func buildTaskPrompt(t domain.Task, p *config.Project, parent *domain.Task, conflicts []string) string {
	isReview := t.Kind == domain.KindReview && parent != nil
	var b strings.Builder
	if isReview {
		fmt.Fprintf(&b, "# Review pull request #%d: %s\n\n", parent.PRNumber, parent.Title)
	} else {
		fmt.Fprintf(&b, "# Task: %s\n\n", t.Title)
	}

	fmt.Fprintf(&b, "- Task ID: %s\n", t.ID)
	fmt.Fprintf(&b, "- Kind: %s\n", t.Kind)
	fmt.Fprintf(&b, "- Project: %s\n", t.Project)
	if p != nil {
		fmt.Fprintf(&b, "- Repository: %s\n", p.Repo)
	}
	fmt.Fprintf(&b, "- Branch: %s\n", t.Branch)
	if t.PRNumber != 0 {
		fmt.Fprintf(&b, "- PR: #%d %s\n", t.PRNumber, t.PRURL)
	}
	if t.Source != "" {
		fmt.Fprintf(&b, "- Source: %s\n", t.Source)
	}
	if t.SourceRef != "" {
		fmt.Fprintf(&b, "- Source ref: %s\n", t.SourceRef)
	}
	b.WriteString("\n")

	if n, ok := strings.CutPrefix(t.SourceRef, "github_issue:"); ok {
		fmt.Fprintf(&b, "This task comes from GitHub issue #%s.\n\n", n)
	}
	if isReview {
		b.WriteString("## The task the change is meant to accomplish\n\n")
		b.WriteString(strings.TrimSpace(parent.Body) + "\n\n")
	}

	if len(conflicts) > 0 {
		base := "the default branch"
		if p != nil {
			base = "origin/" + p.DefaultBranch
		}
		fmt.Fprintf(&b, "## Merge conflicts to resolve\n\nOnlet merged %s into this branch and git reported conflicts in:\n", base)
		for _, f := range conflicts {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\nResolve every conflict marker in these files. Do not commit; onlet commits when you finish.\n\n")
	}

	b.WriteString("## Details\n\n")
	if body := strings.TrimSpace(t.Body); body != "" {
		b.WriteString(body + "\n")
	} else {
		b.WriteString("(no details)\n")
	}
	return b.String()
}
