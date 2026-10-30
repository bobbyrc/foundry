#!/bin/sh
# Stand-in for claude that reads TWO stdin lines before printing one result line whose
# "result" holds both message contents joined by "+". Used to prove a mid-run Send is read.
read -r a || exit 1
read -r b || exit 1
ca=$(printf '%s' "$a" | sed -n 's/.*"content":"\(.*\)"}}$/\1/p')
cb=$(printf '%s' "$b" | sed -n 's/.*"content":"\(.*\)"}}$/\1/p')
printf '{"type":"result","subtype":"success","is_error":false,"result":"%s+%s","session_id":"s-two","total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1}}\n' "$ca" "$cb"
while read -r _; do :; done
