# Plugins

Foundry's built-in integrations are Go packages. Anything else can be added without recompiling
by writing an external program that speaks a small protocol. This page covers the harness plugin
(`exec`); other plugin kinds are documented alongside their packages.

## Harness plugins (exec)

A harness runs one coding-agent CLI non-interactively and turns its output into a stream of
events. The `exec` harness runs any program as that CLI. Use it for an agent foundry does not
ship with.

### Registering a plugin

Add a harness block to `foundry.yaml`. `command` is the program and its fixed leading arguments.
`args` and `env` are added to every session of that harness.

```yaml
harnesses:
  mytool:
    type: exec
    command: ["/path/to/plugin"]
    args: ["--quiet"]
    env:
      MYTOOL_HOME: /opt/mytool
```

An agent profile that names `mytool` as its harness runs through this plugin.

### What foundry gives the plugin

For each session foundry starts the command with:

| Source                | What                                                                              |
|-----------------------|-----------------------------------------------------------------------------------|
| argv                  | `command`, then the harness `args`, then the agent profile's `args`                |
| working directory     | the run's git worktree                                                            |
| stdin                 | the prompt (the same text as in `FOUNDRY_PROMPT_FILE`)                            |
| `FOUNDRY_PROMPT_FILE` | path to a file holding the task prompt                                            |
| `FOUNDRY_INSTRUCTIONS_FILE` | path to a file holding the system instructions (role, rules, outbox contract); empty if none |
| `FOUNDRY_MODEL`       | the model name from the profile; empty if none                                    |
| `FOUNDRY_RESUME_SESSION` | a session ID to continue; empty for a new session                              |
| `env`                 | the harness `env`, then the profile `env`, added to the inherited environment      |

The two prompt files are temporary. Foundry deletes them once the process has exited and its
result has been collected. The plugin must not expect them to exist after it exits.

Everything the agent does must happen inside the working directory. Put any report for foundry
in `.foundry-run/result.json`, as described in the design doc. The protocol below is only for the
live stream.

### Output protocol

The plugin writes to stdout, one JSON object per line. Each object has a `type`:

| `type`        | Meaning                                  | Fields used                       |
|---------------|------------------------------------------|-----------------------------------|
| `text`        | assistant prose                          | `text`                            |
| `tool_use`    | the agent called a tool                  | `text`, for example `shell: ls`   |
| `tool_result` | a tool returned                          | `text`                            |
| `denial`      | a tool call was refused                  | `text`, the refused call          |
| `system`      | bookkeeping shown in the log             | `text`                            |
| `error`       | something failed                         | `text`                            |
| `result`      | the final line; see below                | the fields below                  |

Any object may also carry `session_id`. Foundry remembers the latest one and returns it in the
result, so a later run can resume the session.

Lines that are not JSON are shown as `text`. Lines with an unknown `type` are shown as `system`.

The final line is `type: "result"`:

```json
{"type":"result","session_id":"abc123","text":"Fixed the parser and added tests.","cost_usd":0.12,"input_tokens":5400,"output_tokens":900,"is_error":false,"denials":[]}
```

| Field           | Meaning                                                              |
|-----------------|----------------------------------------------------------------------|
| `text`          | the agent's final message                                            |
| `session_id`    | the session to resume later                                          |
| `cost_usd`      | total cost in US dollars, if known                                   |
| `input_tokens`, `output_tokens` | token counts, if known                                 |
| `is_error`      | true if the run failed                                               |
| `denials`       | descriptions of refused tool calls; a denial stops the run's task    |

If the plugin exits without a result line, foundry uses the last `text` line as the final message.
The run is an error if the exit code is non-zero. The exit code is always recorded.

### Minimal example

This plugin wraps a hypothetical CLI, `mytool`, that prints its progress as plain text and
accepts the prompt on stdin:

```sh
#!/bin/sh
# /path/to/plugin: run mytool on the prompt and report in the exec protocol.
# Needs jq to quote the progress lines.
prompt=$(cat)   # the prompt arrives on stdin; it is also in $FOUNDRY_PROMPT_FILE
out=$(mktemp)
printf '{"type":"system","text":"mytool starting"}\n'
if mytool --model "${FOUNDRY_MODEL:-default}" "$prompt" >"$out" 2>&1; then
	failed=false
else
	failed=true
fi
jq -Rc '{type: "text", text: .}' "$out"
rm -f "$out"
printf '{"type":"result","text":"mytool finished","is_error":%s}\n' "$failed"
```

A real plugin can be as small as one line per event. Keep stdout for protocol lines only, and
send diagnostics to stderr. Stderr is shown in the run log as `stderr` lines.
