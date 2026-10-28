package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

const (
	maxCommandChars = 500  // shell command shown in the log
	maxOutputChars  = 2000 // command output shown in the log
	maxRawChars     = 500  // unrecognised lines shown in the log
)

// streamLine is one line of `codex exec --json`. Its "type" is thread.started, turn.started,
// turn.completed, turn.failed, item.started, item.completed or error.
type streamLine struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Message  string          `json:"message"`
	Item     json.RawMessage `json:"item"`
	Usage    *usage          `json:"usage"`
	Error    *errorBody      `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
}

type usage struct {
	// InputTokens already includes CachedInputTokens, so only InputTokens is counted.
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// item is the payload of item.started and item.completed. Fields are used per item type.
type item struct {
	Type             string   `json:"type"`
	Text             string   `json:"text"`
	Command          string   `json:"command"`
	AggregatedOutput string   `json:"aggregated_output"`
	ExitCode         *int     `json:"exit_code"`
	Changes          []change `json:"changes"`
	Message          string   `json:"message"`
}

type change struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// parser turns codex JSONL into events and keeps what the final Result needs.
type parser struct {
	sessionID string
	announced bool
	lastText  string
	// turnFailed is set by turn.failed, which always fails the run.
	turnFailed bool
	// errorPending is set by a top-level error event and cleared by turn.completed. Codex also
	// emits non-fatal errors (for example "Reconnecting... 1/5") that a successful turn recovers
	// from, so an error alone does not fail the run.
	errorPending bool
	input        int64
	output       int64
}

func newParser() *parser {
	return &parser{}
}

// parseLine handles one stdout line. Unknown lines become system events; parsing never fails.
func (p *parser) parseLine(line string) []harness.Event {
	var sl streamLine
	if err := json.Unmarshal([]byte(line), &sl); err != nil || sl.Type == "" {
		return p.attach([]harness.Event{event(domain.LogSystem, harness.Truncate(line, maxRawChars))})
	}

	var events []harness.Event
	switch sl.Type {
	case "thread.started":
		if sl.ThreadID != "" && sl.ThreadID != p.sessionID {
			p.sessionID = sl.ThreadID
			p.announced = false
		}
		events = append(events, event(domain.LogSystem, "session started (thread "+sl.ThreadID+")"))
	case "turn.started":
	case "turn.completed":
		p.errorPending = false
		if sl.Usage != nil {
			p.input += sl.Usage.InputTokens
			p.output += sl.Usage.OutputTokens
		}
	case "turn.failed":
		msg := "turn failed"
		if sl.Error != nil && sl.Error.Message != "" {
			msg = sl.Error.Message
		}
		p.turnFailed = true
		events = append(events, event(domain.LogError, msg))
	case "error":
		p.errorPending = true
		events = append(events, event(domain.LogError, sl.Message))
	case "item.started":
		events = p.itemEvents(sl.Item, true)
	case "item.completed":
		events = p.itemEvents(sl.Item, false)
	default:
		events = append(events, event(domain.LogSystem, harness.Truncate(line, maxRawChars)))
	}
	return p.attach(events)
}

// attach puts the thread ID on the first emitted event after it is first seen.
func (p *parser) attach(events []harness.Event) []harness.Event {
	if len(events) > 0 && !p.announced && p.sessionID != "" {
		events[0].SessionID = p.sessionID
		p.announced = true
	}
	return events
}

// itemEvents maps one item. Commands are a tool_use when they start and a tool_result when they
// finish. File changes and agent messages are reported once, when they complete.
func (p *parser) itemEvents(raw json.RawMessage, started bool) []harness.Event {
	var it item
	if json.Unmarshal(raw, &it) != nil {
		return nil
	}
	switch it.Type {
	case "agent_message":
		if started || it.Text == "" {
			return nil
		}
		p.lastText = it.Text
		return []harness.Event{event(domain.LogText, it.Text)}
	case "command_execution":
		if started {
			return []harness.Event{event(domain.LogToolUse, "shell: "+harness.Truncate(it.Command, maxCommandChars))}
		}
		text := "exit ?"
		if it.ExitCode != nil {
			text = fmt.Sprintf("exit %d", *it.ExitCode)
		}
		if out := harness.Truncate(it.AggregatedOutput, maxOutputChars); out != "" {
			text += "\n" + out
		}
		return []harness.Event{event(domain.LogToolResult, text)}
	case "file_change":
		if started {
			return nil
		}
		paths := make([]string, 0, len(it.Changes))
		for _, c := range it.Changes {
			paths = append(paths, c.Path)
		}
		return []harness.Event{event(domain.LogToolUse, "edit: "+strings.Join(paths, ", "))}
	case "reasoning":
		return nil
	case "error":
		// Warnings such as a model-metadata fallback. They do not fail the run.
		if started {
			return nil
		}
		return []harness.Event{event(domain.LogSystem, it.Message)}
	default:
		if started {
			return nil
		}
		return []harness.Event{event(domain.LogSystem, harness.Truncate(string(raw), maxRawChars))}
	}
}

// result builds the Result once the process has exited.
func (p *parser) result(exitCode int, stderrTail []string) harness.Result {
	res := harness.Result{
		SessionID:    p.sessionID,
		ExitCode:     exitCode,
		FinalText:    p.lastText,
		InputTokens:  p.input,
		OutputTokens: p.output,
		IsError:      p.turnFailed || p.errorPending || exitCode != 0,
	}
	if p.lastText == "" && exitCode != 0 && len(stderrTail) > 0 {
		res.FinalText = strings.Join(stderrTail, "\n")
	}
	return res
}

func event(kind domain.LogKind, text string) harness.Event {
	return harness.Event{Kind: kind, Text: text}
}
