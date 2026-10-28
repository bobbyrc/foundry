package github

import (
	"regexp"
	"strings"
)

// mentionAtStart matches a GitHub @mention at the start of its input: an @, a login, and an
// optional /team-slug. A login is alphanumerics and hyphens, with no leading or trailing
// hyphen.
var mentionAtStart = regexp.MustCompile(`^@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:/[A-Za-z0-9._-]+)?`)

// quoteMentions wraps every @mention outside code in backticks, so text foundry posts for an agent
// cannot notify anyone. GitHub does not notify for a mention inside code, but it does for a bare
// one, and agent text is written by a model that has no reason to know that. Code is left alone:
// fenced blocks are copied as they are, and so are inline code spans, because a package name such
// as @types/node in code is not a person. Anything that is not a mention, such as #123 issue
// references, is copied byte for byte.
func quoteMentions(s string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.SplitAfter(s, "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			// A fence line opens or closes a block, and the block's lines are copied unchanged.
			inFence = !inFence
			b.WriteString(line)
		case inFence:
			b.WriteString(line)
		default:
			b.WriteString(quoteMentionsInLine(line))
		}
	}
	return b.String()
}

// quoteMentionsInLine quotes the mentions in one line of prose and skips inline code spans. The
// line is scanned byte by byte, which is safe for UTF-8 because every byte it inspects is ASCII.
func quoteMentionsInLine(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		switch line[i] {
		case '`':
			// A span runs to the next backtick on the same line. Without a closing backtick the
			// lone one is ordinary text.
			if end := strings.IndexByte(line[i+1:], '`'); end >= 0 {
				b.WriteString(line[i : i+end+2])
				i += end + 2
				continue
			}
		case '@':
			// The character before an @ decides whether it is a mention. A letter, digit or
			// underscore means an email address or a word, and a backtick means the mention is
			// already quoted.
			if i > 0 && isMentionBlocker(line[i-1]) {
				break
			}
			if m := mentionAtStart.FindString(line[i:]); m != "" {
				b.WriteString("`" + m + "`")
				i += len(m)
				continue
			}
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// isMentionBlocker reports whether a byte before an @ stops it from being a mention.
func isMentionBlocker(c byte) bool {
	return c == '_' || c == '`' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}
