package exec

import (
	"encoding/json"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

// protoLine is one stdout line of the plugin protocol. Type selects the other fields that are
// used: text, session_id and the result fields (cost_usd, input_tokens, output_tokens, is_error,
// denials) on the final "result" line.
type protoLine struct {
	Type         string   `json:"type"`
	Text         string   `json:"text"`
	SessionID    string   `json:"session_id"`
	CostUSD      float64  `json:"cost_usd"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	IsError      bool     `json:"is_error"`
	Denials      []string `json:"denials"`
}

// parser turns protocol lines into events and keeps what the final Result needs.
type parser struct {
	sessionID string
	announced bool
	lastText  string
	denials   []string // text of each denial event, used when the result line lists none
	final     *protoLine
}

func newParser() *parser {
	return &parser{}
}

// parseLine handles one stdout line. A line that is not JSON is plain text. Parsing never
// fails.
func (p *parser) parseLine(line string) []harness.Event {
	var pl protoLine
	if err := json.Unmarshal([]byte(line), &pl); err != nil {
		return p.attach(p.textEvent(line))
	}
	if pl.SessionID != "" && pl.SessionID != p.sessionID {
		p.sessionID = pl.SessionID
		p.announced = false
	}

	var events []harness.Event
	switch pl.Type {
	case "text":
		events = p.textEvent(pl.Text)
	case "tool_use":
		events = []harness.Event{event(domain.LogToolUse, pl.Text)}
	case "tool_result":
		events = []harness.Event{event(domain.LogToolResult, pl.Text)}
	case "denial":
		p.denials = append(p.denials, pl.Text)
		events = []harness.Event{event(domain.LogDenial, pl.Text)}
	case "system":
		events = []harness.Event{event(domain.LogSystem, pl.Text)}
	case "error":
		events = []harness.Event{event(domain.LogError, pl.Text)}
	case "result":
		events = p.resultEvents(pl)
	case "":
		events = []harness.Event{event(domain.LogSystem, harness.Truncate(line, 500))}
	default:
		// Unknown types are kept visible as system lines, so a newer plugin does not vanish.
		text := pl.Text
		if text == "" {
			text = harness.Truncate(line, 500)
		}
		events = []harness.Event{event(domain.LogSystem, text)}
	}
	return p.attach(events)
}

// textEvent returns a text event and remembers it as the latest assistant text.
func (p *parser) textEvent(text string) []harness.Event {
	if text == "" {
		return nil
	}
	p.lastText = text
	return []harness.Event{event(domain.LogText, text)}
}

// resultEvents records the final line. Its denials are reported as denial events, as on every
// other denial, and it does not add a visible message of its own.
func (p *parser) resultEvents(pl protoLine) []harness.Event {
	final := pl
	p.final = &final
	var events []harness.Event
	for _, d := range pl.Denials {
		events = append(events, event(domain.LogDenial, d))
	}
	return events
}

// attach puts the session ID on the first emitted event after it is first seen.
func (p *parser) attach(events []harness.Event) []harness.Event {
	if len(events) > 0 && !p.announced && p.sessionID != "" {
		events[0].SessionID = p.sessionID
		p.announced = true
	}
	return events
}

// result builds the Result once the process has exited. Without a result line, the final text
// is the last text event and the outcome follows the exit code alone.
func (p *parser) result(exitCode int) harness.Result {
	res := harness.Result{
		SessionID: p.sessionID,
		ExitCode:  exitCode,
		FinalText: p.lastText,
		IsError:   exitCode != 0,
	}
	if f := p.final; f != nil {
		if f.Text != "" {
			res.FinalText = f.Text
		}
		res.SessionID = nonEmpty(f.SessionID, p.sessionID)
		res.CostUSD = f.CostUSD
		res.InputTokens = f.InputTokens
		res.OutputTokens = f.OutputTokens
		res.IsError = f.IsError || exitCode != 0
		res.Denials = f.Denials
		if len(res.Denials) == 0 {
			res.Denials = p.denials
		}
	} else {
		res.Denials = p.denials
	}
	return res
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func event(kind domain.LogKind, text string) harness.Event {
	return harness.Event{Kind: kind, Text: text}
}
