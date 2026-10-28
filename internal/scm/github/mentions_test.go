package github

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Z is the zero-width space that neutralizeMention writes after an @.
const Z = "​"

func TestQuoteMentions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain mention", "ping @alice please", "ping @" + Z + "alice please"},
		{"mention at start", "@bob at start", "@" + Z + "bob at start"},
		{"team mention", "team @org/core-team", "team @" + Z + "org/core-team"},
		{"email unchanged", "mail a@b.com", "mail a@b.com"},
		{"already quoted mention neutralised", "already `@carol` quoted", "already `@" + Z + "carol` quoted"},
		{"issue reference unchanged", "Fixes #12", "Fixes #12"},
		{
			"fenced block skipped",
			"```\n@types/node\n```\nthen @dave",
			"```\n@types/node\n```\nthen @" + Z + "dave",
		},
		{
			"inline code neutralised",
			"use `npm i @types/node` and ask @erin",
			"use `npm i @" + Z + "types/node` and ask @" + Z + "erin",
		},
		{"trailing at sign unchanged", "trailing @", "trailing @"},
		{"parenthesised mention", "(@frank)", "(@" + Z + "frank)"},
		{"unclosed fence runs to end", "```\n@grace", "```\n@grace"},
		{"multiline prose", "@one\nand @two.", "@" + Z + "one\nand @" + Z + "two."},
		{"hyphenated login", "cc @a-b-c!", "cc @" + Z + "a-b-c!"},
		{"lone backtick before mention", "`@victim hi", "`@" + Z + "victim hi"},
		{"escaped backticks around mention", "\\`@victim\\`", "\\`@" + Z + "victim\\`"},
		{"longer run is not closed by shorter", "a ``` @victim ` b", "a ``` @" + Z + "victim ` b"},
		{"fence info with backtick is inline code", "```x```\n@victim", "```x```\n@" + Z + "victim"},
		{"indented fence", "  ```\n@v\n```\n@w", "  ```\n@v\n```\n@" + Z + "w"},
		{"tilde fence", "~~~\n@inside\n~~~\nthen @out", "~~~\n@inside\n~~~\nthen @" + Z + "out"},
		{"double backtick span neutralised", "``code with ` and @x``", "``code with ` and @" + Z + "x``"},
		{"escaped at sign unchanged", "\\@handle", "\\@handle"},
		{"escaped backslash does not escape mention", "\\\\@handle", "\\\\@" + Z + "handle"},
		{"shorter fence run does not close", "````\n```\n@in\n````\n@out", "````\n```\n@in\n````\n@" + Z + "out"},
		{"escaped backtick in inline code neutralised", "`a\\`@x`", "`a\\`@" + Z + "x`"},
		{"mention before double backtick", "@x``y", "@" + Z + "x``y"},
		{"code span across lines", "`foo\n@bar`", "`foo\n@" + Z + "bar`"},
		{"two-line code span pairing", "x ` y\n`@z` w", "x ` y\n`@" + Z + "z` w"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, quoteMentions(tc.in))
		})
	}
}

// bareMention matches an @ that GitHub would link: one at the start of text or after a non-word
// byte, directly followed by a login character.
var bareMention = regexp.MustCompile(`(^|[^A-Za-z0-9_])@[A-Za-z0-9]`)

// TestQuoteMentionsNeverLeavesBareMention checks inputs with no fenced block, where no @login may
// remain unneutralized in the output.
func TestQuoteMentionsNeverLeavesBareMention(t *testing.T) {
	prose := []string{
		"ping @alice please",
		"`@victim hi",
		"\\`@victim\\`",
		"a ``` @victim ` b",
		"`a\\`@x`",
		"@x``y",
		"`foo\n@bar`",
		"x ` y\n`@z` w",
		"@one\nand @two.",
		"(@frank)",
	}
	for _, in := range prose {
		t.Run(in, func(t *testing.T) {
			assert.False(t, bareMention.MatchString(quoteMentions(in)), "bare mention in %q", quoteMentions(in))
		})
	}
}

func TestCommentIssueQuotesMentions(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	require.NoError(t, g.CommentIssue(context.Background(), 8, "ask @alice, see #12"))
	require.Len(t, f.calls, 1)
	assert.Equal(t, "ask @"+Z+"alice, see #12", f.calls[0].stdin)
}
