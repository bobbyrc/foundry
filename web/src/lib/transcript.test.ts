import { describe, expect, it } from 'vitest';
import type { ChatMessage, LogKind, LogLine, Run, Task, TaskConversation } from '../types';
import { clipLines, orchestratorEntries, parseTool, runEntries, taskEntries } from './transcript';

let seq = 0;
function line(kind: LogKind, text: string, tool_id?: string, run_id = 'r1'): LogLine {
  seq += 1;
  return { run_id, seq, time: `2026-01-01T00:00:${String(seq).padStart(2, '0')}Z`, kind, text, tool_id };
}

describe('parseTool', () => {
  it('parses Bash command', () => {
    expect(parseTool('Bash: {"command":"go test ./...","description":"run"}')).toEqual({
      name: 'Bash',
      arg: 'go test ./...',
    });
  });
  it('counts edit lines', () => {
    const t = parseTool('Edit: {"file_path":"a.go","old_string":"x\\ny","new_string":"a\\nb\\nc"}');
    expect(t).toEqual({ name: 'Edit', arg: 'a.go', added: 3, removed: 2 });
  });
  it('does not count a trailing newline as a line', () => {
    const t = parseTool('Edit: {"file_path":"a.go","old_string":"x\\n","new_string":"a\\nb\\n"}');
    expect(t).toMatchObject({ added: 2, removed: 1 });
  });
  it('stringifies nested argument values', () => {
    expect(parseTool('Grep: {"pattern":{"re":"x"}}')).toEqual({ name: 'Grep', arg: '{"re":"x"}' });
  });
  it('extracts the argument from truncated input', () => {
    expect(parseTool('Bash: {"command":"onett task create --title \\"x\\" --body hel')).toEqual({
      name: 'Bash',
      arg: 'onett task create --title "x" --body hel',
    });
    expect(parseTool('Read: {"file_path":"/a/b.go","limit":')).toEqual({ name: 'Read', arg: '/a/b.go' });
  });
  it('falls back on truncated input', () => {
    expect(parseTool('Bash: {"cmd":"echo hel')).toEqual({ name: 'Bash', arg: '{"cmd":"echo hel' });
  });
});

describe('runEntries', () => {
  it('pairs results by tool_id', () => {
    const e = runEntries([
      line('tool_use', 'Read: {"file_path":"a"}', 'A'),
      line('tool_use', 'Read: {"file_path":"b"}', 'B'),
      line('tool_result', 'second', 'B'),
      line('tool_result', 'error: first failed', 'A'),
    ]);
    expect(e).toHaveLength(2);
    expect(e[0]).toMatchObject({ kind: 'tool', result: 'error: first failed', resultError: true });
    expect(e[1]).toMatchObject({ kind: 'tool', result: 'second' });
  });
  it('pairs results by order when tool_id is missing', () => {
    const e = runEntries([
      line('tool_use', 'Bash: {"command":"a"}'),
      line('tool_result', 'one'),
      line('tool_use', 'Bash: {"command":"b"}'),
      line('tool_result', 'two'),
    ]);
    expect(e.map((x) => (x.kind === 'tool' ? x.result : ''))).toEqual(['one', 'two']);
  });
  it('pairs parallel calls without ids in call order', () => {
    const e = runEntries([
      line('tool_use', 'Bash: {"command":"a"}'),
      line('tool_use', 'Bash: {"command":"b"}'),
      line('tool_result', 'r1'),
      line('tool_result', 'r2'),
    ]);
    expect(e.map((x) => (x.kind === 'tool' ? x.result : ''))).toEqual(['r1', 'r2']);
  });
  it('uses the task body when no prompt line exists', () => {
    const e = runEntries([line('text', 'hi')], 'do the thing');
    expect(e[0]).toMatchObject({ kind: 'prompt', label: 'Task (prompt not recorded)', text: 'do the thing' });
    const withPrompt = runEntries([line('prompt', 'given'), line('text', 'hi')], 'do the thing');
    expect(withPrompt[0]).toMatchObject({ kind: 'prompt', label: 'Prompt', text: 'given' });
    expect(withPrompt).toHaveLength(2);
  });
});

const msg = (p: Partial<ChatMessage> & Pick<ChatMessage, 'id' | 'role' | 'text'>): ChatMessage => ({
  conversation_id: 'c1',
  created_at: '2026-01-01T00:00:00Z',
  ...p,
});

describe('orchestratorEntries', () => {
  it('puts orchestrator turn logs after their message', () => {
    const logs = { r1: [line('prompt', 'hello'), line('text', 'reply')] };
    const e = orchestratorEntries(
      [
        msg({ id: 'm1', role: 'user', text: 'hello', run_id: 'r1' }),
        msg({ id: 'm2', role: 'assistant', text: 'reply', run_id: 'r1' }),
      ],
      logs,
    );
    expect(e.map((x) => x.kind)).toEqual(['owner', 'text']);
    // Without logs the assistant message itself is shown.
    const noLogs = orchestratorEntries(
      [
        msg({ id: 'm1', role: 'user', text: 'hello', run_id: 'r1' }),
        msg({ id: 'm2', role: 'assistant', text: 'reply', run_id: 'r1' }),
      ],
      {},
    );
    expect(noLogs.map((x) => x.kind)).toEqual(['owner', 'text']);
  });
  it('shows queued messages last', () => {
    const e = orchestratorEntries(
      [
        msg({ id: 'q', role: 'user', text: 'later', queued: true }),
        msg({ id: 'm1', role: 'user', text: 'now', run_id: 'r1' }),
      ],
      { r1: [line('text', 'ok')] },
    );
    expect(e[e.length - 1]).toMatchObject({ kind: 'owner', text: 'later', queued: true, messageId: 'q' });
    expect(e[0]).toMatchObject({ kind: 'owner', text: 'now' });
  });
});

describe('taskEntries', () => {
  const run = (id: string, session_id?: string) => ({ id, task_id: 't', session_id, started_at: '2026-01-01T00:00:00Z' }) as Run;
  const conv = (runs: Run[], messages: ChatMessage[] = []) =>
    ({ task: { id: 't', title: 'T', body: 'body' } as Task, tasks: [], runs, messages, can_send: true }) as TaskConversation;

  it('marks a new session with a divider', () => {
    const e = taskEntries(conv([run('r1', 's1'), run('r2', 's2')]), {
      r1: [line('prompt', 'p1', undefined, 'r1')],
      r2: [line('prompt', 'p2', undefined, 'r2')],
    });
    expect(e.map((x) => x.kind)).toEqual(['prompt', 'divider', 'prompt']);
    expect(e[1]).toMatchObject({ text: 'New session · retried by owner' });
    const same = taskEntries(conv([run('r1', 's1'), run('r2', 's1')]), { r1: [line('text', 'a', undefined, 'r1')], r2: [] });
    expect(same.some((x) => x.kind === 'divider')).toBe(false);
  });
  it('shows queued messages last', () => {
    const e = taskEntries(
      conv([run('r1')], [msg({ id: 'q', role: 'user', text: 'wait', queued: true }), msg({ id: 'm', role: 'user', text: 'sent', run_id: 'r1' })]),
      { r1: [line('text', 'a', undefined, 'r1')] },
    );
    expect(e.map((x) => x.kind)).toEqual(['prompt', 'text', 'owner']);
    expect(e[2]).toMatchObject({ text: 'wait', queued: true, messageId: 'q' });
  });
});

describe('entry identity', () => {
  it('carries the run id and a stable key built from run and seq', () => {
    const a = line('text', 'one', undefined, 'rA');
    const b = line('tool_use', 'Bash: {"command":"ls"}', 'T', 'rA');
    const e = runEntries([a, b]);
    expect(e.map((x) => x.runId)).toEqual(['rA', 'rA']);
    expect(e.map((x) => x.id)).toEqual([`rA:${a.seq}`, `rA:${b.seq}`]);
  });
  it('keys messages by message id and every entry uniquely', () => {
    const e = orchestratorEntries(
      [msg({ id: 'm1', role: 'user', text: 'hi', run_id: 'r1' }), msg({ id: 'q', role: 'user', text: 'later', queued: true })],
      { r1: [line('text', 'a', undefined, 'r1'), line('text', 'b', undefined, 'r1')] },
    );
    expect(e[0]).toMatchObject({ id: 'm:m1' });
    expect(e[e.length - 1]).toMatchObject({ id: 'm:q' });
    expect(new Set(e.map((x) => x.id)).size).toBe(e.length);
  });
  it('tags the fallback assistant text with its run', () => {
    const e = orchestratorEntries(
      [msg({ id: 'm1', role: 'user', text: 'hi', run_id: 'r1' }), msg({ id: 'm2', role: 'assistant', text: 'yo', run_id: 'r1' })],
      {},
    );
    expect(e[1]).toMatchObject({ kind: 'text', runId: 'r1' });
  });
});

describe('a message delivered mid-run', () => {
  it('shows the run log once, and the message through the log line that carries it', () => {
    const logs = {
      r1: [line('prompt', 'first', undefined, 'r1'), line('text', 'working', undefined, 'r1'), line('owner', 'also this', undefined, 'r1')],
    };
    const e = orchestratorEntries(
      [msg({ id: 'm1', role: 'user', text: 'first', run_id: 'r1' }), msg({ id: 'm2', role: 'user', text: 'also this', run_id: 'r1' })],
      logs,
    );
    expect(e.map((x) => x.kind)).toEqual(['owner', 'text', 'owner']);
    expect(e.filter((x) => x.kind === 'owner').map((x) => x.kind === 'owner' && x.text)).toEqual(['first', 'also this']);
  });
  it('keeps a second message that rode the same turn but has no log line', () => {
    const e = orchestratorEntries(
      [msg({ id: 'm1', role: 'user', text: 'one', run_id: 'r1' }), msg({ id: 'm2', role: 'user', text: 'two', run_id: 'r1' })],
      { r1: [line('prompt', 'one\n\ntwo', undefined, 'r1'), line('text', 'ok', undefined, 'r1')] },
    );
    expect(e.map((x) => x.kind)).toEqual(['owner', 'text', 'owner']);
    expect(e[2]).toMatchObject({ text: 'two' });
  });
});

describe('clipLines', () => {
  it('does not count a trailing newline as a line', () => {
    expect(clipLines('a\nb\nc\n', 3)).toEqual({ lines: ['a', 'b', 'c'], clipped: false });
  });
  it('clips to the preview length', () => {
    expect(clipLines('a\nb\nc\nd\n', 3)).toEqual({ lines: ['a', 'b', 'c'], clipped: true });
    expect(clipLines('a\nb\nc\nd', 3, true)).toEqual({ lines: ['a', 'b', 'c', 'd'], clipped: true });
  });
});
