package codex

import (
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/assert"
)

const (
	lineCompleted = `{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`
	lineFailed    = `{"type":"turn.failed","error":{"message":"turn broke"}}`
	lineError     = `{"type":"error","message":"Reconnecting... 1/5"}`
)

// feed parses lines in order and returns the Result for an exit code of 0, plus every LogError
// text the parser emitted.
func feed(t *testing.T, lines ...string) (errTexts []string, isError bool) {
	t.Helper()
	p := newParser()
	for _, line := range lines {
		for _, ev := range p.parseLine(line) {
			if ev.Kind == domain.LogError {
				errTexts = append(errTexts, ev.Text)
			}
		}
	}
	return errTexts, p.result(0, nil).IsError
}

func TestErrorFollowedByCompletedIsNotAnError(t *testing.T) {
	errTexts, isError := feed(t, lineError, lineCompleted)
	assert.Equal(t, []string{"Reconnecting... 1/5"}, errTexts, "the error is still recorded")
	assert.False(t, isError, "a turn that completed after a non-fatal error succeeded")
}

func TestCompletedThenErrorIsAnError(t *testing.T) {
	errTexts, isError := feed(t, lineCompleted, lineError)
	assert.Equal(t, []string{"Reconnecting... 1/5"}, errTexts)
	assert.True(t, isError, "an error with no turn.completed after it fails the run")
}

func TestErrorAloneIsAnError(t *testing.T) {
	_, isError := feed(t, lineError)
	assert.True(t, isError)
}

func TestTurnFailedIsAnErrorEvenAfterCompleted(t *testing.T) {
	_, isError := feed(t, lineError, lineCompleted, lineFailed)
	assert.True(t, isError, "turn.failed always fails the run")
}

func TestTurnFailedAloneIsAnError(t *testing.T) {
	errTexts, isError := feed(t, lineFailed)
	assert.Equal(t, []string{"turn broke"}, errTexts)
	assert.True(t, isError)
}

func TestCleanStreamIsNotAnError(t *testing.T) {
	_, isError := feed(t, lineCompleted)
	assert.False(t, isError)
}

func TestCachedInputTokensAreSummed(t *testing.T) {
	p := newParser()
	p.parseLine(`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":5}}`)
	p.parseLine(`{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":10,"output_tokens":5}}`)
	res := p.result(0, nil)
	assert.Equal(t, int64(150), res.InputTokens)
	assert.Equal(t, int64(50), res.CachedInputTokens)
}
