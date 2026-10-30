package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// errRunNotLive means the run is not registered (it never started or has already ended), so
// there is no agent to hand a message to.
var errRunNotLive = errors.New("run is not live")

// setSession records the live harness session so owner messages can reach it. The run
// supervisor calls it right after h.Start succeeds.
func (ar *activeRun) setSession(s harness.Session) {
	ar.sessMu.Lock()
	ar.sess = s
	ar.sessMu.Unlock()
}

func (ar *activeRun) session() harness.Session {
	ar.sessMu.Lock()
	defer ar.sessMu.Unlock()
	return ar.sess
}

// ownerPrompt is the text an agent sees for owner messages: a fixed heading, then each message.
func ownerPrompt(texts []string) string {
	return "## Message from the owner\n\n" + strings.Join(texts, "\n\n")
}

// deliverToRun sends an owner message to the live agent of run runID and logs the raw text as an
// owner line, but only once the agent has accepted it. It returns errRunNotLive when the run is
// not registered or has no session yet, and harness.ErrInputClosed when the session does not
// take input or has stopped taking it; the caller then queues the message.
func (e *Engine) deliverToRun(ctx context.Context, runID, text string) error {
	e.runsMu.Lock()
	ar, ok := e.active[runID]
	e.runsMu.Unlock()
	if !ok {
		return errRunNotLive
	}
	sess := ar.session()
	if sess == nil {
		return errRunNotLive
	}
	st, ok := sess.(harness.Steerable)
	if !ok {
		return harness.ErrInputClosed
	}
	// Send can block until the agent reads, so the registry lock is not held here.
	if err := st.Send(ownerPrompt([]string{text})); err != nil {
		if errors.Is(err, harness.ErrInputClosed) {
			return harness.ErrInputClosed
		}
		return fmt.Errorf("send to agent: %w", err)
	}
	e.appendRunLog(ctx, runID, domain.LogOwner, text)
	return nil
}
