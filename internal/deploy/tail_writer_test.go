package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTailWriterKeepsWholeOutputWhenNothingDropped(t *testing.T) {
	w := &tailWriter{max: 100}
	_, _ = w.Write([]byte("a\nb\n"))
	assert.Equal(t, "a\nb\n", w.String())
}

func TestTailWriterDropsPartialFirstLineAfterTruncation(t *testing.T) {
	// 24 bytes into a 10-byte tail: the kept bytes start in the middle of "line3", so that line
	// must go and the cut is marked.
	w := &tailWriter{max: 10}
	_, _ = w.Write([]byte("line1\nline2\nline3\nline4\n"))
	assert.Equal(t, "[…]\nline4\n", w.String())
}

func TestTailWriterDropsEverythingWhenNoNewline(t *testing.T) {
	w := &tailWriter{max: 4}
	_, _ = w.Write([]byte("abcdefghijklmnop"))
	assert.Equal(t, "[…]", w.String())
}

// The buffer is trimmed inside Write, leaving exactly max bytes: the tail must still count as
// truncated, or its first partial line would be shown as if it were whole.
func TestTailWriterMarksTruncationWhenTrimmedInWrite(t *testing.T) {
	w := &tailWriter{max: 5}
	_, _ = w.Write([]byte("ab\ncdefghi"))
	_, _ = w.Write([]byte("j"))
	assert.Equal(t, "[…]", w.String())
}

func TestDropPartialLineUnchangedWhenNotDropped(t *testing.T) {
	assert.Equal(t, "partial\nwhole", DropPartialLine("partial\nwhole", false))
	assert.Equal(t, "", DropPartialLine("", false))
}

func TestDropPartialLineCutsThroughFirstNewline(t *testing.T) {
	assert.Equal(t, "[…]\nwhole\n", DropPartialLine("frag\nwhole\n", true))
	assert.Equal(t, "[…]\n", DropPartialLine("frag\n", true))
	assert.Equal(t, "[…]", DropPartialLine("frag", true))
}
