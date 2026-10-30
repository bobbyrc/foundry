#!/bin/sh
# Stand-in for claude with --input-format stream-json. Prints one result line per stdin line,
# with "result" set to that line's message.content, and exits when stdin closes. The CLI
# arguments are reported on stderr.
for a in "$@"; do
	printf 'arg=%s\n' "$a" >&2
done
while IFS= read -r line; do
	text=$(printf '%s' "$line" | sed -n 's/.*"content":"\(.*\)"}}$/\1/p')
	printf '{"type":"result","subtype":"success","is_error":false,"result":"%s","session_id":"s-echo","total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1}}\n' "$text"
done
