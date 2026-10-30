#!/bin/sh
# Stand-in for the claude CLI in unit tests. $1 is the fixture to replay; the remaining
# arguments are the CLI arguments onett built, which are reported on stderr with the stdin.
fixture=$1
shift
IFS= read -r input
for a in "$@"; do
	printf 'arg=%s\n' "$a" >&2
done
printf 'stdin=%s\n' "$input" >&2
cat "$fixture"
