// Turns stored chat messages and agent run logs into an ordered list of transcript entries.
// Pure logic: no React, no fetching. The UI renders the entries like a coding-agent transcript.
import type { ChatMessage, LogLine, TaskConversation } from '../types';

export interface ToolSummary {
  name: string;
  arg: string;
  added?: number;
  removed?: number;
}

/**
 * Every entry has a stable `id` (React key) and, when it came from a run's log, the `runId`.
 * `afterRun` in the Transcript uses `runId` to put a chip after the last entry of a run.
 */
interface EntryBase {
  id: string;
  runId?: string;
  time: string;
}

export type Entry = EntryBase &
  (
    | { kind: 'owner'; text: string; queued?: boolean; messageId?: string }
    | { kind: 'prompt'; text: string; label: string }
    | { kind: 'text'; text: string }
    | { kind: 'tool'; tool: ToolSummary; input: string; result?: string; resultError?: boolean }
    | { kind: 'denial' | 'error' | 'system' | 'stderr'; text: string }
    | { kind: 'divider'; text: string }
  );

const ARG_KEYS = ['command', 'file_path', 'path', 'pattern', 'url', 'query', 'description'];

function countLines(s: unknown): number {
  if (typeof s !== 'string' || s === '') return 0;
  return s.replace(/\n$/, '').split('\n').length;
}

function argText(v: unknown): string {
  return typeof v === 'string' ? v : JSON.stringify(v);
}

/** The first known argument of truncated JSON input: its string value up to the closing quote or the end. */
function partialArg(raw: string): string | undefined {
  for (const key of ARG_KEYS) {
    const m = new RegExp(`"${key}"\\s*:\\s*"((?:[^"\\\\]|\\\\.)*)`).exec(raw);
    if (!m) continue;
    // A value cut off mid-escape ends in a lone backslash; drop it.
    const body = m[1].replace(/\\$/, '');
    return body.replace(/\\(["\\n]|.)/g, (_, c: string) => (c === 'n' ? '\n' : c));
  }
  return undefined;
}

/** Reads a tool_use line, which is `Name: <compact JSON input>` and may be truncated. */
export function parseTool(text: string): ToolSummary {
  const i = text.indexOf(': ');
  if (i < 0) return { name: text, arg: '' };
  const name = text.slice(0, i);
  const raw = text.slice(i + 2);
  let input: unknown;
  try {
    input = JSON.parse(raw);
  } catch {
    return { name, arg: partialArg(raw) ?? raw };
  }
  if (input === null || typeof input !== 'object' || Array.isArray(input)) return { name, arg: raw };
  const obj = input as Record<string, unknown>;
  const key = ARG_KEYS.find((k) => obj[k] !== undefined && obj[k] !== null && obj[k] !== '');
  const tool: ToolSummary = { name, arg: key ? argText(obj[key]) : '' };
  if (name === 'Edit') {
    tool.added = countLines(obj.new_string);
    tool.removed = countLines(obj.old_string);
  }
  return tool;
}

function linesToEntries(lines: LogLine[], skipPrompt: boolean, fallbackPrompt?: string): Entry[] {
  if (lines.length === 0) return [];
  const sorted = [...lines].sort((a, b) => a.seq - b.seq);
  const out: Entry[] = [];
  const byId = new Map<string, Extract<Entry, { kind: 'tool' }>>();
  const open: Extract<Entry, { kind: 'tool' }>[] = []; // calls still waiting for a result

  if (!sorted.some((l) => l.kind === 'prompt') && !skipPrompt && fallbackPrompt) {
    out.push({
      id: `${sorted[0].run_id}:prompt`,
      runId: sorted[0].run_id,
      kind: 'prompt',
      text: fallbackPrompt,
      time: sorted[0].time,
      label: 'Task (prompt not recorded)',
    });
  }

  for (const l of sorted) {
    const at = { id: `${l.run_id}:${l.seq}`, runId: l.run_id, time: l.time };
    switch (l.kind) {
      case 'prompt':
        if (!skipPrompt) out.push({ ...at, kind: 'prompt', text: l.text, label: 'Prompt' });
        break;
      case 'owner':
        out.push({ ...at, kind: 'owner', text: l.text });
        break;
      case 'text':
        out.push({ ...at, kind: 'text', text: l.text });
        break;
      case 'tool_use': {
        const call: Extract<Entry, { kind: 'tool' }> = { ...at, kind: 'tool', tool: parseTool(l.text), input: l.text };
        out.push(call);
        open.push(call);
        if (l.tool_id) byId.set(l.tool_id, call);
        break;
      }
      case 'tool_result': {
        let call = l.tool_id ? byId.get(l.tool_id) : undefined;
        if (!call || call.result !== undefined) call = open[0]; // oldest first: parallel calls finish in call order
        if (!call) {
          // A result with no call before it: show it on its own.
          call = { ...at, kind: 'tool', tool: { name: 'Result', arg: '' }, input: '' };
          out.push(call);
        } else {
          open.splice(open.indexOf(call), 1);
        }
        call.result = l.text;
        if (l.text.startsWith('error: ')) call.resultError = true;
        break;
      }
      default:
        out.push({ ...at, kind: l.kind, text: l.text });
    }
  }
  return out;
}

/** One run's log as entries. `fallbackPrompt` (the task body) stands in when the run recorded no prompt line. */
export function runEntries(lines: LogLine[], fallbackPrompt?: string): Entry[] {
  return linesToEntries(lines, false, fallbackPrompt);
}

function queuedEntries(messages: ChatMessage[]): Entry[] {
  return messages
    .filter((m) => m.queued && m.role === 'user')
    .map((m) => ({ id: `m:${m.id}`, kind: 'owner', text: m.text, time: m.created_at, queued: true, messageId: m.id }));
}

/**
 * Cuts a tool result to `max` lines for a preview. A trailing newline ends the last line rather
 * than starting another, so "a\nb\n" is two lines. `all` shows every line but still reports
 * whether the preview would have clipped.
 */
export function clipLines(result: string, max: number, all = false): { lines: string[]; clipped: boolean } {
  const lines = result.replace(/\n$/, '').split('\n');
  const clipped = lines.length > max;
  return { lines: clipped && !all ? lines.slice(0, max) : lines, clipped };
}

/** An orchestrator conversation: each message, then the log of the run it started. */
export function orchestratorEntries(messages: ChatMessage[], logsByRun: Record<string, LogLine[]>): Entry[] {
  const out: Entry[] = [];
  // Runs whose log is already shown. A message delivered while the run was live names that same
  // run, so its log must not be shown again under the message.
  const shown = new Set<string>();
  for (const m of messages) {
    if (m.queued) continue;
    const logs = m.run_id ? logsByRun[m.run_id] : undefined;
    const hasLogs = !!logs && logs.length > 0;
    if (m.role === 'user') {
      if (m.run_id && shown.has(m.run_id)) {
        // The log carries a mid-run message as an owner line. One that rode the same turn's prompt
        // has no such line, so it still needs its own entry.
        if (logs?.some((l) => l.kind === 'owner' && l.text === m.text)) continue;
        out.push({ id: `m:${m.id}`, kind: 'owner', text: m.text, time: m.created_at, messageId: m.id });
        continue;
      }
      out.push({ id: `m:${m.id}`, kind: 'owner', text: m.text, time: m.created_at, messageId: m.id });
      if (m.run_id && hasLogs) {
        shown.add(m.run_id);
        out.push(...linesToEntries(logs, true));
      }
    } else if (m.role === 'assistant') {
      // The reply is already the last text line of the run's log when the log is loaded.
      if (!hasLogs) out.push({ id: `m:${m.id}`, runId: m.run_id, kind: 'text', text: m.text, time: m.created_at });
    } else {
      out.push({ id: `m:${m.id}`, kind: 'system', text: m.text, time: m.created_at });
    }
  }
  out.push(...queuedEntries(messages));
  return out;
}

/** A task agent's conversation: its runs in order, with a divider where a retry started a new session. */
export function taskEntries(conv: TaskConversation, logsByRun: Record<string, LogLine[]>): Entry[] {
  const out: Entry[] = [];
  conv.runs.forEach((run, i) => {
    const prev = conv.runs[i - 1];
    if (prev && run.session_id && run.session_id !== prev.session_id) {
      out.push({ id: `div:${run.id}`, kind: 'divider', text: 'New session · retried by owner', time: run.started_at });
    }
    out.push(...linesToEntries(logsByRun[run.id] ?? [], false, conv.task.body));
  });
  out.push(...queuedEntries(conv.messages));
  return out;
}
