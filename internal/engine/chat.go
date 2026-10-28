package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

// chatReply runs one orchestrator turn and stores its reply. The caller has already marked the
// conversation busy; this always clears the flag, even when the turn could not start.
func (e *Engine) chatReply(ctx context.Context, conv domain.Conversation, text string) {
	reply, runID, sessionID := e.chatTurn(ctx, conv, text)
	if reply == "" {
		reply = "_(no reply)_"
	}
	// The turn may have timed out; the reply and the busy flag are still written.
	fin := context.WithoutCancel(ctx)
	msg, err := e.d.Store.AppendChatMessage(fin, domain.ChatMessage{
		ConversationID: conv.ID, Role: "assistant", Text: reply, RunID: runID,
	})
	if err != nil {
		e.d.Logger.Error("chat: save reply", "conversation", conv.ID, "err", err)
	} else {
		e.d.Bus.Emit(fin, domain.EvChatMessage, conv.Project, "", runID, "orchestrator replied", msg)
	}
	if _, err := e.d.Store.UpdateConversation(fin, conv.ID, func(c *domain.Conversation) error {
		if sessionID != "" {
			c.SessionID = sessionID
		}
		c.Busy = false
		return nil
	}); err != nil {
		e.d.Logger.Error("chat: release conversation", "conversation", conv.ID, "err", err)
	}
}

// chatTurn runs the orchestrator harness for one message. It returns the reply text, the run
// ID ("" when no run started) and the harness session ID to resume next turn ("" if unknown).
func (e *Engine) chatTurn(ctx context.Context, conv domain.Conversation, text string) (string, string, string) {
	cfg := e.Config()
	h, agent, err := e.harnessFor("orchestrator")
	if err != nil {
		return "I can't run: " + err.Error(), "", ""
	}
	dir := filepath.Join(cfg.Server.DataDir, "chat", conv.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "I can't run: " + err.Error(), "", ""
	}
	base, err := e.loadPrompt(agent.Prompt)
	if err != nil {
		return "I can't run: " + err.Error(), "", ""
	}

	run, err := e.d.Store.CreateRun(ctx, domain.Run{
		Project: conv.Project, Role: "orchestrator", Harness: agent.Harness, Model: agent.Model,
		Status: domain.RunStarting, Workdir: dir,
	})
	if err != nil {
		return "I can't run: " + err.Error(), "", ""
	}
	e.d.Bus.Emit(ctx, domain.EvRunUpdated, conv.Project, "", run.ID, "orchestrator chat run starting", run)

	runCtx, cancel := context.WithTimeout(ctx, cfg.Limits.RunTimeout.Duration)
	defer cancel()
	ar := e.registerRun(run.ID, "", conv.Project, cancel)
	defer e.unregisterRun(run.ID)

	env := make(map[string]string, len(e.d.AgentEnv)+3)
	for k, v := range e.d.AgentEnv {
		env[k] = v
	}
	env["FOUNDRY_CONVERSATION_ID"] = conv.ID
	env["FOUNDRY_ROLE"] = "orchestrator"
	env["FOUNDRY_RUN_ID"] = run.ID

	sess, err := h.Start(runCtx, harness.Spec{
		RunID:           run.ID,
		Workdir:         dir,
		Prompt:          text,
		Instructions:    chatInstructions(cfg, conv, base),
		Model:           agent.Model,
		ResumeSessionID: conv.SessionID,
		Env:             env,
		Args:            agent.Args,
	})
	if err != nil {
		e.failChatRun(ctx, run, err)
		return "I couldn't start: " + err.Error(), run.ID, ""
	}

	res, status, reason := e.supervise(runCtx, run, sess, ar, func(ev harness.Event) {
		if ev.Kind != domain.LogText || ev.Text == "" {
			return
		}
		// The map holds only strings, so it cannot fail to marshal.
		data, _ := json.Marshal(map[string]string{
			"conversation_id": conv.ID, "run_id": run.ID, "text": ev.Text,
		})
		if _, err := e.d.Bus.Publish(ctx, domain.Event{
			Type: domain.EvChatDelta, Project: conv.Project, RunID: run.ID, Data: data,
		}); err != nil {
			e.d.Logger.Warn("chat: publish delta", "err", err)
		}
	})

	reply := res.FinalText
	if status != domain.RunSucceeded {
		note := "_(The orchestrator run ended: " + string(status)
		if reason != "" {
			note += ": " + reason
		}
		note += ".)_"
		if reply != "" {
			reply += "\n\n"
		}
		reply += note
	}
	return reply, run.ID, res.SessionID
}

// failChatRun marks a chat run that never started as failed, so it does not stay "starting".
func (e *Engine) failChatRun(ctx context.Context, run domain.Run, cause error) {
	now := e.d.Now().UTC()
	r, err := e.d.Store.UpdateRun(ctx, run.ID, func(r *domain.Run) error {
		r.Status, r.Error, r.EndedAt = domain.RunFailed, cause.Error(), &now
		return nil
	})
	if err != nil {
		e.d.Logger.Error("chat: fail run", "run", run.ID, "err", err)
		return
	}
	e.d.Bus.Emit(ctx, domain.EvRunUpdated, r.Project, "", r.ID, "orchestrator chat run failed to start", r)
}

// chatInstructions is the orchestrator's system prompt: its role prompt plus a snapshot of the
// projects and agents that exist right now.
func chatInstructions(cfg *config.Config, conv domain.Conversation, base string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n## The factory right now\n\n")
	for _, name := range cfg.ProjectNames() {
		p := cfg.Projects[name]
		paused := "no"
		if p.Paused {
			paused = "yes"
		}
		fmt.Fprintf(&b, "- **%s** — %s, %s, paused: %s\n", name, p.Repo, p.Path, paused)
	}

	agents := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		agents = append(agents, name)
	}
	sort.Strings(agents)
	b.WriteString("\nAgents:\n")
	for _, name := range agents {
		a := cfg.Agents[name]
		who := a.Harness
		if a.Model != "" {
			who += "/" + a.Model
		}
		fmt.Fprintf(&b, "- **%s** (%s) — %s; handles: %s\n", name, who, a.Description, handledKinds(cfg, name))
	}
	if conv.Project != "" {
		fmt.Fprintf(&b, "\nDefault project for this conversation: %s.", conv.Project)
	}
	return b.String()
}

// handledKinds lists the task kinds routed to an agent, or "none".
func handledKinds(cfg *config.Config, agent string) string {
	var kinds []string
	for kind, name := range cfg.Routing {
		if name == agent {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return "none"
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}
