import { describe, expect, it } from 'vitest';
import type { ChatMessage, LogKind, LogLine, Run, Task, TaskConversation } from '../types';
import { orchestratorEntries, parseTool, runEntries, taskEntries } from './transcript';

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
  it('falls back on truncated input', () => {
    expect(parseTool('Bash: {"command":"echo hel')).toEqual({ name: 'Bash', arg: '{"command":"echo hel' });
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
