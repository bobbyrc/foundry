package github

import (
	"regexp"
	"strings"
)

// mentionAtStart matches a GitHub @mention at the start of its input: an @, a login, and an
// optional /team-slug. A login is alphanumerics and hyphens, with no leading or trailing
// hyphen.
var mentionAtStart = regexp.MustCompile(`^@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9._-]+)?`)

// neutralizeMention is the zero-width space written between an @ and a login. GitHub only links an
// @ that is directly followed by a login character, so the space stops the mention from being
// linked, and the text still reads the same. Backticks would not do: whether a backtick pairs with
// another depends on the whole paragraph, which a line-by-line scan cannot see, so wrapping a
// mention can leave it bare.
const neutralizeMention = "​"

// quoteMentions stops every @mention from notifying anyone, so text foundry posts for an agent
// cannot ping people. GitHub notifies for a mention anywhere it is not inside a fenced code block,
// and agent text is written by a model that has no reason to know that. Only fenced blocks are
// copied byte for byte, because they hold commands people copy. Everywhere else, including inline
// code, a mention gets a zero-width space after its @: an invisible character in an inline snippet
// is cosmetic, while a missed ping is not. Anything that is not a mention, such as #123 issue
// references, is copied byte for byte. Fence rules follow CommonMark: see openingFence.
func quoteMentions(s string) string {
	var b strings.Builder
	var fence codeFence // zero value: no fenced block is open
	for _, line := range strings.SplitAfter(s, "\n") {
		switch {
		case fence.open():
			// The block's lines, including its closing fence, are copied unchanged.
			if fence.closedBy(line) {
				fence = codeFence{}
			}
			b.WriteString(line)
		default:
			if f, ok := openingFence(line); ok {
				fence = f
				b.WriteString(line)
				continue
			}
			b.WriteString(neutralizeMentionsInLine(line))
		}
	}
	return b.String()
}

// codeFence is an open fenced code block: its fence character and the length of its opening run.
type codeFence struct {
	char   byte
	length int
}

// open reports whether the fence is a real block; the zero value is not.
func (f codeFence) open() bool { return f.length > 0 }

// openingFence reports whether a line opens a fenced code block. After up to three spaces of
// indentation it must start with a run of at least three backticks or tildes. For backticks, the
// rest of the line (the info string) may not contain a backtick: "```x```" is inline code, not a
// fence.
func openingFence(line string) (codeFence, bool) {
	indent := leadingSpaces(line)
	if indent > 3 {
		return codeFence{}, false
	}
	rest := line[indent:]
	if rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return codeFence{}, false
	}
	n := leadingRun(rest, rest[0])
	if n < 3 {
		return codeFence{}, false
	}
	if rest[0] == '`' && strings.Contains(rest[n:], "`") {
		return codeFence{}, false
	}
	return codeFence{char: rest[0], length: n}, true
}

// closedBy reports whether a line closes the fence. After up to three spaces of indentation it must
// have a run of the same character at least as long as the opening run, followed by nothing but
// whitespace. A shorter run does not close the block.
func (f codeFence) closedBy(line string) bool {
	indent := leadingSpaces(line)
	if indent > 3 {
		return false
	}
	rest := line[indent:]
	n := leadingRun(rest, f.char)
	return n >= f.length && strings.TrimSpace(rest[n:]) == ""
}

// neutralizeMentionsInLine writes a zero-width space after each mention in one line of text. A
// backslash escape is copied as a pair, so "\@" is not a mention, but "\\@" is, because the
// backslashes escape each other. The line is scanned byte by byte, which is safe for UTF-8 because
// every byte it inspects is ASCII.
func neutralizeMentionsInLine(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && isASCIIPunct(line[i+1]):
			b.WriteString(line[i : i+2])
			i += 2
		case c == '@' && !(i > 0 && isMentionBlocker(line[i-1])) && mentionAtStart.MatchString(line[i:]):
			// The byte before an @ decides whether it is a mention: a letter, digit or underscore
			// means an email address or a word. Anything else, including a backtick, does not stop
			// it.
			m := mentionAtStart.FindString(line[i:])
			b.WriteString("@" + neutralizeMention + m[1:])
			i += len(m)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// leadingRun returns how many bytes at the start of s equal c.
func leadingRun(s string, c byte) int {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n
}

// leadingSpaces returns how many spaces start the line.
func leadingSpaces(line string) int {
	return leadingRun(line, ' ')
}

// isASCIIPunct reports whether c is an ASCII punctuation character, the only bytes a backslash
// escapes in CommonMark.
func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// isMentionBlocker reports whether a byte before an @ stops it from being a mention.
func isMentionBlocker(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}
