package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuoteMentions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain mention", "ping @alice please", "ping `@alice` please"},
		{"mention at start", "@bob at start", "`@bob` at start"},
		{"team mention", "team @org/core-team", "team `@org/core-team`"},
		{"email unchanged", "mail a@b.com", "mail a@b.com"},
		{"already quoted unchanged", "already `@carol` quoted", "already `@carol` quoted"},
		{"issue reference unchanged", "Fixes #12", "Fixes #12"},
		{
			"fenced block skipped",
			"```\n@types/node\n```\nthen @dave",
			"```\n@types/node\n```\nthen `@dave`",
		},
		{
			"inline code skipped",
			"use `npm i @types/node` and ask @erin",
			"use `npm i @types/node` and ask `@erin`",
		},
		{"trailing at sign unchanged", "trailing @", "trailing @"},
		{"parenthesised mention", "(@frank)", "(`@frank`)"},
		{"unclosed fence runs to end", "```\n@grace", "```\n@grace"},
		{"multiline prose", "@one\nand @two.", "`@one`\nand `@two`."},
		{"hyphenated login", "cc @a-b-c!", "cc `@a-b-c`!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, quoteMentions(tc.in))
		})
	}
}

func TestCommentIssueQuotesMentions(t *testing.T) {
	f := newFake()
	g := NewWithRunner(repo, f.runner())
	require.NoError(t, g.CommentIssue(context.Background(), 8, "ask @alice, see #12"))
	require.Len(t, f.calls, 1)
	assert.Equal(t, "ask `@alice`, see #12", f.calls[0].stdin)
}
