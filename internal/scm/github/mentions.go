package github

import (
	"regexp"
	"strings"
)

// mentionAtStart matches a GitHub @mention at the start of its input: an @, a login, and an
// optional /team-slug. A login is alphanumerics and hyphens, with no leading or trailing
// hyphen.
var mentionAtStart = regexp.MustCompile(`^@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9._-]+)?`)

// atSignEntity matches an HTML character reference that GitHub decodes to a literal @ before it
// links mentions: &#64;, &#x40; (any case of x, optional leading zeros) and &commat;.
var atSignEntity = regexp.MustCompile(`^&(?:#0*64|#[xX]0*40|commat);`)

// neutralizeMention is the zero-width space (U+200B) written between an @ and a login. GitHub only
// links an @ that is directly followed by a login character, so the space stops the mention from
// being linked, and the text still reads the same. Backticks would not do: whether a backtick pairs
// with another depends on the whole paragraph, which a line-by-line scan cannot see, so wrapping a
// mention can leave it bare.
const neutralizeMention = "\u200b"

// quoteMentions stops every @mention from notifying anyone, so text foundry posts for an agent
// cannot ping people. Agent text is written by a model that has no reason to know which spellings
// GitHub links, so the rule is conservative. Only a fenced code block is copied byte for byte,
// because it holds commands people copy. Everywhere else, including inline code, a mention gets a
// zero-width space after its @: an invisible character in an inline snippet is cosmetic, while a
// missed ping is not. The same goes for a backslash escape (\@login) and an HTML character
// reference for @ (&#64;, &#x40;, &commat;) that is followed by a login character, because GitHub
// turns both into a literal @ before it links mentions. Anything else, such as #123 issue
// references, is copied byte for byte.
//
// Which fences are skipped is decided by skippedFences. GitHub parses fences by CommonMark rules,
// where a fence inside a list item or after an HTML block can be closed early or read as something
// else. Lines that GitHub then treats as prose would ping while being copied raw, so a fence is
// skipped only when its placement is unambiguous. Otherwise its lines are scanned like any others.
func quoteMentions(s string) string {
	lines := strings.SplitAfter(s, "\n")
	fences := skippedFences(lines)
	var b strings.Builder
	for i := 0; i < len(lines); {
		if end, ok := fences[i]; ok {
			// The block's lines, including its opening and closing fences, are copied unchanged.
			for ; i <= end; i++ {
				b.WriteString(lines[i])
			}
			continue
		}
		b.WriteString(neutralizeMentionsInLine(lines[i]))
		i++
	}
	return b.String()
}

// skippedFences returns the fenced blocks that quoteMentions copies unchanged, keyed by the index of
// the opening line, with the index of the closing line as the value. A fence is skipped only when
// all of these hold:
//   - the opening line starts at column 0, so it is not inside a list item or other container;
//   - the line before it is blank, or it is the first line, or the previous line closed another
//     skipped fence;
//   - the first later line that CommonMark would accept as a closer is at column 0 and closes it.
//
// The last rule means a closer indented by one to three spaces, which CommonMark accepts, stops the
// block from being skipped, because GitHub may end the block there and treat the lines after it as
// prose. An opening fence that never closes is not a fence at all: its lines are ordinary text.
func skippedFences(lines []string) map[int]int {
	fences := map[int]int{}
	closedPrev := false
	for i := 0; i < len(lines); {
		if f, ok := openingFence(lines[i]); ok && leadingSpaces(lines[i]) == 0 &&
			(i == 0 || closedPrev || strings.TrimSpace(lines[i-1]) == "") {
			if end := fenceEnd(lines, i, f); end >= 0 {
				fences[i] = end
				closedPrev = true
				i = end + 1
				continue
			}
		}
		closedPrev = false
		i++
	}
	return fences
}

// fenceEnd returns the index of the line that closes the fence opened at line open, or -1 when the
// fence is not valid. The first line after the opening that closes the fence decides the result: it
// must be at column 0, otherwise -1 is returned even if a later column-0 closer exists.
func fenceEnd(lines []string, open int, f codeFence) int {
	for k := open + 1; k < len(lines); k++ {
		if f.closedBy(lines[k]) {
			if leadingSpaces(lines[k]) == 0 {
				return k
			}
			return -1
		}
	}
	return -1
}

// codeFence is an open fenced code block: its fence character and the length of its opening run.
type codeFence struct {
	char   byte
	length int
}

// openingFence reports whether a line opens a fenced code block. After up to three spaces of
// indentation it must start with a run of at least three backticks or tildes. For backticks, the
// rest of the line (the info string) may not contain a backtick: "```x```" is inline code, not a
// fence. The caller decides whether indentation is allowed; skippedFences requires none.
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

// closedBy reports whether a line closes the fence, using the CommonMark rule. After up to three
// spaces of indentation it must have a run of the same character at least as long as the opening
// run, followed by nothing but whitespace. A shorter run does not close the block.
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
// backslash escape is copied as a pair, so "\`" is not special, but "\\@" is a mention, because the
// backslashes escape each other. "\@login" is a mention too, since GitHub drops the backslash, so
// the space goes after the @ and the backslash stays. An HTML reference for @ followed by a login
// character is replaced by "@" and the space. The line is scanned byte by byte, which is safe for
// UTF-8 because every byte it inspects is ASCII.
func neutralizeMentionsInLine(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && line[i+1] == '@':
			// The byte before the backslash decides, as it does for a plain @.
			if m := mentionAtStart.FindString(line[i+1:]); m != "" && !precededByBlocker(line, i) {
				b.WriteString("\\@" + neutralizeMention + m[1:])
				i += 1 + len(m)
			} else {
				b.WriteString(line[i : i+2])
				i += 2
			}
		case c == '\\' && i+1 < len(line) && isASCIIPunct(line[i+1]):
			b.WriteString(line[i : i+2])
			i += 2
		case c == '&' && !precededByBlocker(line, i) && atSignEntityLen(line[i:]) > 0:
			b.WriteString("@" + neutralizeMention)
			i += atSignEntityLen(line[i:])
		case c == '@' && !precededByBlocker(line, i) && mentionAtStart.MatchString(line[i:]):
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

// atSignEntityLen returns the length of the HTML reference for @ that starts s, when a login
// character follows it, and 0 otherwise. A reference with no login after it cannot link.
func atSignEntityLen(s string) int {
	m := atSignEntity.FindString(s)
	if m == "" || len(m) >= len(s) || !isLoginChar(s[len(m)]) {
		return 0
	}
	return len(m)
}

// precededByBlocker reports whether the byte before position i stops the mention, escape or
// reference at i from counting, as in an email address.
func precededByBlocker(line string, i int) bool {
	return i > 0 && isMentionBlocker(line[i-1])
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

// isLoginChar reports whether c is an ASCII letter or digit, the characters a login can start with.
func isLoginChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// isMentionBlocker reports whether a byte before an @ stops it from being a mention.
func isMentionBlocker(c byte) bool {
	return c == '_' || isLoginChar(c)
}
