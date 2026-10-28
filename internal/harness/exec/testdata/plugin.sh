#!/bin/sh
# Test plugin for foundry's exec harness. It speaks the protocol documented in docs/plugins.md.
# The first word of the prompt selects the behaviour:
#   full  every line type, then a result line
#   bare  text lines only, no result line
#   fail  one text line, then exits 4
#   env   reports the environment it was given
read -r mode _rest < "$FOUNDRY_PROMPT_FILE"
cat > /dev/null
printf '{"type":"system","text":"args=%s"}\n' "$*"
case "$mode" in
full)
	printf '{"type":"text","text":"thinking"}\n'
	printf '{"type":"tool_use","text":"shell: ls"}\n'
	printf '{"type":"tool_result","text":"a.txt"}\n'
	printf '{"type":"denial","text":"Bash: rm -rf /"}\n'
	printf '{"type":"error","text":"soft error"}\n'
	printf 'plain text line\n'
	printf '{"type":"result","session_id":"plug-1","text":"all done","cost_usd":0.1,"input_tokens":1,"output_tokens":2,"is_error":false,"denials":["Bash: rm -rf /"]}\n'
	;;
bare)
	printf '{"type":"text","text":"half way"}\n'
	printf '{"type":"text","text":"last words","session_id":"plug-2"}\n'
	;;
fail)
	printf '{"type":"text","text":"about to fail"}\n'
	exit 4
	;;
env)
	printf '{"type":"system","text":"model=%s resume=%s"}\n' "$FOUNDRY_MODEL" "$FOUNDRY_RESUME_SESSION"
	printf '{"type":"system","text":"prompt_file=%s"}\n' "$FOUNDRY_PROMPT_FILE"
	printf '{"type":"system","text":"instructions_file=%s"}\n' "$FOUNDRY_INSTRUCTIONS_FILE"
	printf '{"type":"text","text":"instructions=%s"}\n' "$(cat "$FOUNDRY_INSTRUCTIONS_FILE")"
	printf '{"type":"result","text":"env ok"}\n'
	;;
esac
exit 0
