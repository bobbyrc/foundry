package github

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Z is the zero-width space that neutralizeMention writes after an @.
const Z = "\u200b"

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
		{"unclosed fence is text", "```\n@grace", "```\n@" + Z + "grace"},
		{"multiline prose", "@one\nand @two.", "@" + Z + "one\nand @" + Z + "two."},
		{"hyphenated login", "cc @a-b-c!", "cc @" + Z + "a-b-c!"},
		{"lone backtick before mention", "`@victim hi", "`@" + Z + "victim hi"},
		{"escaped backticks around mention", "\\`@victim\\`", "\\`@" + Z + "victim\\`"},
		{"longer run is not closed by shorter", "a ``` @victim ` b", "a ``` @" + Z + "victim ` b"},
		{"fence info with backtick is inline code", "```x```\n@victim", "```x```\n@" + Z + "victim"},
		{"indented fence is text", "  ```\n@v\n```\n@w", "  ```\n@" + Z + "v\n```\n@" + Z + "w"},
		{"tilde fence", "~~~\n@inside\n~~~\nthen @out", "~~~\n@inside\n~~~\nthen @" + Z + "out"},
		{"double backtick span neutralised", "``code with ` and @x``", "``code with ` and @" + Z + "x``"},
		{"escaped at sign neutralised", "\\@handle", "\\@" + Z + "handle"},
		{"escaped backslash does not escape mention", "\\\\@handle", "\\\\@" + Z + "handle"},
		{"shorter fence run does not close", "````\n```\n@in\n````\n@out", "````\n```\n@in\n````\n@" + Z + "out"},
		{"escaped backtick in inline code neutralised", "`a\\`@x`", "`a\\`@" + Z + "x`"},
		{"mention before double backtick", "@x``y", "@" + Z + "x``y"},
		{"code span across lines", "`foo\n@bar`", "`foo\n@" + Z + "bar`"},
		{"two-line code span pairing", "x ` y\n`@z` w", "x ` y\n`@" + Z + "z` w"},
		{
			"fence in list item is text",
			"- item\n  ```\n  code\n\n@bob please",
			"- item\n  ```\n  code\n\n@" + Z + "bob please",
		},
		{
			"fence after html block is text",
			"<details>\n```\n@x\n```\n</details>",
			"<details>\n```\n@" + Z + "x\n```\n</details>",
		},
		{"fence with no blank line before is text", "text\n```\n@x\n```", "text\n```\n@" + Z + "x\n```"},
		{
			"fence after blank line is skipped",
			"intro\n\n```\nnpm i @types/node\n```\nthen @bob",
			"intro\n\n```\nnpm i @types/node\n```\nthen @" + Z + "bob",
		},
		{
			"fence directly after a closed fence is skipped",
			"```\na\n```\n```\n@x\n```\n@y",
			"```\na\n```\n```\n@x\n```\n@" + Z + "y",
		},
		{
			"indented closer stops the fence being skipped",
			"```\n@x\n  ```\n@y\n```",
			"```\n@" + Z + "x\n  ```\n@" + Z + "y\n```",
		},
		{"backslash at sign with letter before unchanged", "a\\@bob", "a\\@bob"},
		{"html decimal at sign", "&#64;bob", "@" + Z + "bob"},
		{"html hex at sign", "&#x40;bob", "@" + Z + "bob"},
		{"html hex at sign with upper x and zeros", "&#X0040;bob", "@" + Z + "bob"},
		{"html named at sign", "&commat;bob", "@" + Z + "bob"},
		{"html at sign with letter before unchanged", "a&#64;b.com", "a&#64;b.com"},
		{"html at sign with no login after unchanged", "&#64; bob", "&#64; bob"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, quoteMentions(tc.in))
		})
	}
}

// bareMention matches an @, or an HTML reference to one, that GitHub would link: an @ at the start of
// text or after a non-word byte, or a reference to @, directly followed by a login character.
var bareMention = regexp.MustCompile(`(^|[^A-Za-z0-9_])@[A-Za-z0-9]|&#0*64;[A-Za-z0-9]|&#[xX]0*40;[A-Za-z0-9]|&commat;[A-Za-z0-9]`)

// TestQuoteMentionsNeverLeavesBareMention checks inputs with no fenced block, where no @login may
// remain unneutralized in the output.
func TestQuoteMentionsNeverLeavesBareMention(t *testing.T) {
	prose := []string{
		"ping @alice please",
		"`@victim hi",
		"\\`@victim\\`",
		"\\@bob",
		"\\\\@bob",
		"&#64;bob",
		"&#x40;bob",
		"&#X0040;bob",
		"&commat;bob",
		"a ``` @victim ` b",
		"`a\\`@x`",
		"@x``y",
		"`foo\n@bar`",
		"x ` y\n`@z` w",
		"@one\nand @two.",
		"(@frank)",
		"```\n@unclosed",
		"- item\n  ```\n  code\n\n@bob please",
		"<details>\n```\n@x\n```\n</details>",
		"text\n```\n@x\n```",
		"```\n@x\n  ```\n@y\n```",
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
