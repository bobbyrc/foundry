package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostile is text an agent or a GitHub issue could supply: a window-title escape (OSC), a colour
// change (CSI) and a clear-screen.
const hostile = "\x1b]0;pwned\x07title\x1b[31m"

func TestCleanTextRemovesTerminalSequences(t *testing.T) {
	assert.Equal(t, "title", cleanText(hostile))
	assert.Equal(t, "ab", cleanText("a\x1b[2Jb"), "CSI with parameters")
	assert.Equal(t, "x", cleanText("\x1b]8;;http://e\x1b\\x\x1b]8;;\x1b\\"), "OSC ended by ST")
	assert.Equal(t, "ab", cleanText("a\x1bMb"), "two-byte ESC sequence")
	assert.Equal(t, "abc", cleanText("a\u009b31mb\u009d0;x\u0007c"), "C1 CSI and C1 OSC")
}

func TestCleanTextRemovesControlsButKeepsLinesAndTabs(t *testing.T) {
	assert.Equal(t, "one\ttwo\nthree", cleanText("one\ttwo\nthree\x00\x07\x08\r\x7f"))
	assert.Equal(t, "é", cleanText("\u0085é"), "NEL is C1 and goes; printable text stays")
}

func TestOneLineFlattensLinesAndTabs(t *testing.T) {
	assert.Equal(t, "a b c", oneLine("a\nb\tc\x1b[0m"))
}

func TestTruncateIsCleanAndOneLine(t *testing.T) {
	assert.Equal(t, "title", truncate(hostile, 60))
	assert.Equal(t, "a b", truncate("a\n\tb", 60))
}

func TestTableCellsCannotCarryEscapes(t *testing.T) {
	var b bytes.Buffer
	table(&b, []string{"ID", "TITLE"}, [][]string{{"t-1", hostile + "\nsecond"}})
	assert.NotContains(t, b.String(), "\x1b")
	assert.NotContains(t, b.String(), "pwned")
	assert.Equal(t, 2, strings.Count(b.String(), "\n"), "a newline in a cell does not add a row")
}

func TestPrintJSONEscapesC1Controls(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, printJSON(&b, map[string]string{"title": "a\u009bb"}))
	assert.NotContains(t, b.String(), "\u009b")
	assert.Contains(t, b.String(), `a\u009bb`)
	var back map[string]string
	require.NoError(t, json.Unmarshal(b.Bytes(), &back), "still valid JSON")
	assert.Equal(t, "a\u009bb", back["title"], "the value means the same")
}

func TestTaskShowStripsEscapesFromTaskText(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks/t-1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, client.TaskDetail{Task: domain.Task{
			ID: "t-1", Title: hostile, Body: "body\x1b[2J\nline two", Summary: hostile,
		}})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("task", "show", "t-1")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Task t-1: title")
	assert.Contains(t, out, "body\nline two")
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "pwned")
}

func TestEventsStripEscapesFromMessages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events/history", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []domain.Event{{ID: 1, Type: "task.updated", Message: "\x1b[2Jhello\nworld"}})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("events")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "task.updated")
	assert.Contains(t, out, "hello world")
	assert.NotContains(t, out, "\x1b")
}

func TestRunLogsStripEscapesFromLines(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/runs/r-1/logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []domain.LogLine{{RunID: "r-1", Seq: 1, Kind: "stdout", Text: hostile}})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("run", "logs", "r-1")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "stdout title")
	assert.NotContains(t, out, "\x1b")
}

func TestErrorMessagesFromTheServerAreCleaned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks/t-1/cancel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "\x1b]0;x\x07busy"})
	})
	fakeAPI(t, mux)

	code, _, errOut := runCLI("task", "cancel", "t-1")
	assert.Equal(t, 1, code)
	assert.Equal(t, "onett: busy\n", errOut)
}

func TestChatReplyKeepsLinesAndDropsEscapes(t *testing.T) {
	old := chatPollInterval
	chatPollInterval = time.Millisecond
	t.Cleanup(func() { chatPollInterval = old })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/conversations", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, domain.Conversation{ID: "c-1"})
	})
	mux.HandleFunc("POST /api/v1/conversations/c-1/messages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, domain.ChatMessage{Role: "user", Text: "hi"})
	})
	mux.HandleFunc("GET /api/v1/conversations/c-1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, client.ConversationDetail{
			Conversation: domain.Conversation{ID: "c-1"},
			Messages: []domain.ChatMessage{{Role: "assistant", Text: hostile + "\nsecond line",
				CreatedAt: time.Now()}},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("chat", "hi")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "title\nsecond line\n", out)
	assert.NotContains(t, out, "\x1b")
}
