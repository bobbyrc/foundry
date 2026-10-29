package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// frictionMatchThreshold is the Jaccard similarity of two titles' token sets at which they count
// as the same problem.
const frictionMatchThreshold = 0.5

// frictionStopWords are dropped from titles before comparing them.
var frictionStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "to": true, "of": true, "in": true,
	"on": true, "for": true, "with": true, "and": true, "or": true, "by": true, "at": true,
	"from": true, "it": true, "this": true, "that": true, "be": true, "not": true, "no": true,
	"so": true, "as": true,
}

// frictionSuffixes are stripped by frictionStem, in order.
var frictionSuffixes = []string{"ing", "ed", "er", "es", "s"}

// frictionTokens turns a title into the set of its significant stems: lowercase words split on
// anything that is not a letter or digit, without stop words and words shorter than three
// characters.
func frictionTokens(title string) map[string]bool {
	out := map[string]bool{}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		if utf8.RuneCountInString(w) < 3 || frictionStopWords[w] {
			continue
		}
		out[frictionStem(w)] = true
	}
	return out
}

// frictionStem strips the suffixes in frictionSuffixes, repeatedly, while at least four characters
// remain. It is a crude stemmer: "listening" and "listeners" both become "listen".
func frictionStem(w string) string {
	for stripped := true; stripped; {
		stripped = false
		for _, suf := range frictionSuffixes {
			if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 4 {
				w = w[:len(w)-len(suf)]
				stripped = true
				break
			}
		}
	}
	return w
}

// frictionSimilarity is the Jaccard similarity of two token sets: the size of their intersection
// over the size of their union. Two empty sets are not similar.
func frictionSimilarity(a, b map[string]bool) float64 {
	shared := 0
	for t := range a {
		if b[t] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// similarTitle returns the index of the candidate most similar to title, when that similarity is at
// least frictionMatchThreshold. On a tie the earlier candidate wins.
func similarTitle(title string, candidates []string) (int, bool) {
	want := frictionTokens(title)
	best, bestScore := -1, 0.0
	for i, c := range candidates {
		if s := frictionSimilarity(want, frictionTokens(c)); s >= frictionMatchThreshold && s > bestScore {
			best, bestScore = i, s
		}
	}
	return best, best >= 0
}

// frictionLoop files a GitHub issue for each friction report that has reached its threshold and
// has no issue yet. An existing issue with the same title is linked instead of duplicated.
func (e *Engine) frictionLoop(ctx context.Context, p *config.Project) error {
	loops := e.Config().LoopsFor(p.Name)
	if !isGitHubRepo(p) || !loops.FrictionAutoFile() {
		return nil
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("friction: scm: %w", err)
	}
	items, err := e.d.Store.ListFriction(ctx, p.Name, 200)
	if err != nil {
		return fmt.Errorf("friction: list: %w", err)
	}
	var errs []error
	for _, f := range items {
		if f.IssueURL != "" || f.Count < loops.Friction.MinCount {
			continue
		}
		if err := e.fileFriction(ctx, s, p, f, loops); err != nil {
			errs = append(errs, fmt.Errorf("friction %s: %w", f.ID, err))
		}
	}
	if err := e.refreshFrictionStates(ctx, s, loops, items); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// refreshFrictionStates copies each filed issue's current state ("open" or "closed") onto its
// friction report, so the UI can tell resolved problems from open ones.
func (e *Engine) refreshFrictionStates(ctx context.Context, s scm.SCM, loops config.Loops, items []domain.Friction) error {
	issues, err := s.ListIssues(ctx, loops.Friction.Label, "all")
	if err != nil {
		return fmt.Errorf("friction: list issue states: %w", err)
	}
	states := make(map[string]string, len(issues))
	for _, is := range issues {
		states[is.URL] = is.State
	}
	var errs []error
	for _, f := range items {
		st, ok := states[f.IssueURL]
		if f.IssueURL == "" || !ok || st == "" || st == f.IssueState {
			continue
		}
		if _, err := e.d.Store.UpdateFriction(ctx, f.ID, func(x *domain.Friction) error {
			x.IssueState = st
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("friction %s: set state: %w", f.ID, err))
		}
	}
	return errors.Join(errs...)
}

// fileFriction links friction f to an issue with the same title, or creates one, and records
// the issue URL and state on f.
func (e *Engine) fileFriction(ctx context.Context, s scm.SCM, p *config.Project, f domain.Friction, loops config.Loops) error {
	matches, err := s.SearchIssues(ctx, f.Title)
	if err != nil {
		return fmt.Errorf("search issues: %w", err)
	}
	url, state := "", ""
	for _, m := range matches {
		if strings.EqualFold(m.Title, f.Title) {
			url, state = m.URL, m.State
			break
		}
	}
	if url == "" {
		open, err := s.ListIssues(ctx, loops.Friction.Label, "open")
		if err != nil {
			return fmt.Errorf("list open friction issues: %w", err)
		}
		titles := make([]string, len(open))
		for i, is := range open {
			titles[i] = is.Title
		}
		if i, ok := similarTitle(f.Title, titles); ok {
			msg := fmt.Sprintf("Reported again by run `%s`: %s\n\n%s", f.RunID, f.Title, f.Body)
			if err := s.CommentIssue(ctx, open[i].Number, msg); err != nil {
				return fmt.Errorf("comment on issue #%d: %w", open[i].Number, err)
			}
			url, state = open[i].URL, open[i].State
		}
	}
	if url == "" {
		labels := []string{loops.Friction.Label}
		if loops.Friction.AutoQueue {
			labels = append(labels, loops.IssueIntake.Label)
		}
		body := f.Body + fmt.Sprintf("\n\n---\nReported by onett agents %d time(s); first in run `%s`.", f.Count, f.RunID)
		issue, err := s.CreateIssue(ctx, scm.IssueSpec{Title: f.Title, Body: body, Labels: labels})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		url, state = issue.URL, issue.State
		e.d.Bus.Emit(ctx, domain.EvIssueFiled, p.Name, "", "",
			fmt.Sprintf("filed issue #%d: %s", issue.Number, f.Title),
			map[string]any{"number": issue.Number, "url": issue.URL, "title": f.Title})
	}
	if state == "" {
		state = "open"
	}
	_, err = e.d.Store.UpdateFriction(ctx, f.ID, func(x *domain.Friction) error {
		x.IssueURL = url
		x.IssueState = state
		return nil
	})
	return err
}

// frictionFingerprintFor returns the fingerprint that a new friction report with this title should
// be stored under. When the project already has a similar report, that report's fingerprint is
// reused so the count goes up instead of a near-duplicate row being added.
func (e *Engine) frictionFingerprintFor(ctx context.Context, project, title string) string {
	existing, err := e.d.Store.ListFriction(ctx, project, 200)
	if err != nil {
		e.d.Logger.Warn("friction dedupe: list", "project", project, "err", err)
		return frictionFingerprint(title)
	}
	titles := make([]string, len(existing))
	for i, f := range existing {
		titles[i] = f.Title
	}
	if i, ok := similarTitle(title, titles); ok {
		return existing[i].Fingerprint
	}
	return frictionFingerprint(title)
}
