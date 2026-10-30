// Turns stored chat messages and agent run logs into an ordered list of transcript entries.
// Pure logic: no React, no fetching. The UI renders the entries like a coding-agent transcript.
import type { ChatMessage, LogLine, TaskConversation } from '../types';

export interface ToolSummary {
  name: string;
  arg: string;
  added?: number;
  removed?: number;
}

export type Entry =
  | { kind: 'owner'; text: string; time: string; queued?: boolean; messageId?: string }
  | { kind: 'prompt'; text: string; time: string; label: string }
  | { kind: 'text'; text: string; time: string }
  | { kind: 'tool'; tool: ToolSummary; input: string; result?: string; resultError?: boolean; time: string }
  | { kind: 'denial' | 'error' | 'system' | 'stderr'; text: string; time: string }
  | { kind: 'divider'; text: string; time: string };

const ARG_KEYS = ['command', 'file_path', 'path', 'pattern', 'url', 'query', 'description'];

function countLines(s: unknown): number {
  if (typeof s !== 'string' || s === '') return 0;
  return s.replace(/\n$/, '').split('\n').length;
}

function argText(v: unknown): string {
  return typeof v === 'string' ? v : JSON.stringify(v);
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
    return { name, arg: raw };
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
    out.push({ kind: 'prompt', text: fallbackPrompt, time: sorted[0].time, label: 'Task (prompt not recorded)' });
  }

  for (const l of sorted) {
    switch (l.kind) {
      case 'prompt':
        if (!skipPrompt) out.push({ kind: 'prompt', text: l.text, time: l.time, label: 'Prompt' });
        break;
      case 'owner':
        out.push({ kind: 'owner', text: l.text, time: l.time });
        break;
      case 'text':
        out.push({ kind: 'text', text: l.text, time: l.time });
        break;
      case 'tool_use': {
        const call: Extract<Entry, { kind: 'tool' }> = { kind: 'tool', tool: parseTool(l.text), input: l.text, time: l.time };
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
          call = { kind: 'tool', tool: { name: 'Result', arg: '' }, input: '', time: l.time };
          out.push(call);
        } else {
          open.splice(open.indexOf(call), 1);
        }
        call.result = l.text;
        if (l.text.startsWith('error: ')) call.resultError = true;
        break;
      }
      default:
        out.push({ kind: l.kind, text: l.text, time: l.time });
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
    .map((m) => ({ kind: 'owner', text: m.text, time: m.created_at, queued: true, messageId: m.id }));
}

/** An orchestrator conversation: each message, then the log of the run it started. */
export function orchestratorEntries(messages: ChatMessage[], logsByRun: Record<string, LogLine[]>): Entry[] {
  const out: Entry[] = [];
  for (const m of messages) {
    if (m.queued) continue;
    const logs = m.run_id ? logsByRun[m.run_id] : undefined;
    const hasLogs = !!logs && logs.length > 0;
    if (m.role === 'user') {
      out.push({ kind: 'owner', text: m.text, time: m.created_at, messageId: m.id });
      if (hasLogs) out.push(...linesToEntries(logs, true));
    } else if (m.role === 'assistant') {
      // The reply is already the last text line of the run's log when the log is loaded.
      if (!hasLogs) out.push({ kind: 'text', text: m.text, time: m.created_at });
    } else {
      out.push({ kind: 'system', text: m.text, time: m.created_at });
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
      out.push({ kind: 'divider', text: 'New session · retried by owner', time: run.started_at });
    }
    out.push(...linesToEntries(logsByRun[run.id] ?? [], false, conv.task.body));
  });
  out.push(...queuedEntries(conv.messages));
  return out;
}
