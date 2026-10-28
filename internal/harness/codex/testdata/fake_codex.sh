#!/bin/sh
# Stand-in for the codex CLI in unit tests. $1 is the fixture to replay; the remaining
# arguments are the CLI arguments onlet built, which are reported on stderr with the stdin.
fixture=$1
shift
input=$(cat)
for a in "$@"; do
	printf 'arg=%s\n' "$a" >&2
done
printf 'stdin=%s\n' "$input" >&2
cat "$fixture"
