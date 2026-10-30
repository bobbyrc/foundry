import { Fragment, useState, type ReactNode } from 'react';
import { cx, formatClock } from '../lib/format';
import { parseTime } from '../lib/time';
import { clipLines, type Entry } from '../lib/transcript';
import type { TranscriptView } from '../lib/transcriptView';
import { Markdown } from './Markdown';
import './Transcript.css';

const RESULT_PREVIEW_LINES = 3;
const QUEUED_NOTE = 'Queued · sends when the agent finishes this run';

type ToolEntry = Extract<Entry, { kind: 'tool' }>;

function clock(time: string): string {
  const t = parseTime(time);
  return t ? formatClock(t) : '';
}

/** The Conversation / Terminal switch. The caller stores the choice. */
export function ViewToggle({ view, onChange }: { view: TranscriptView; onChange(v: TranscriptView): void }) {
  const options: { value: TranscriptView; label: string }[] = [
    { value: 'conversation', label: 'Conversation' },
    { value: 'terminal', label: 'Terminal' },
  ];
  return (
    <div className="tr-seg" role="group" aria-label="Transcript view">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          className={cx(view === o.value && 'on')}
          aria-pressed={view === o.value}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export interface TranscriptProps {
  entries: Entry[];
  view: TranscriptView;
  /** Who the agent prose belongs to: "Orchestrator", or a task agent's role such as "Implementer". */
  agentLabel: string;
  /** Rendered after the last entry of each run, for example a chip for tasks that run created. */
  afterRun?: (runId: string) => ReactNode;
  /** Rendered in the note of a queued owner message, for Edit and Cancel. */
  queuedActions?: (messageId: string) => ReactNode;
}

/** A run of agent entries, or an owner's turn, laid out as a conversation or as terminal lines. */
export function Transcript(props: TranscriptProps) {
  return props.view === 'terminal' ? <TerminalView {...props} /> : <ConversationView {...props} />;
}

const isAgentKind = (e: Entry) => e.kind === 'text' || e.kind === 'tool';

/** The slot after an entry that ends a run's stretch of entries. */
function runEnd(entries: Entry[], i: number, afterRun: TranscriptProps['afterRun']): ReactNode {
  const runId = entries[i].runId;
  if (!afterRun || !runId || entries[i + 1]?.runId === runId) return null;
  return afterRun(runId);
}

function ConversationView({ entries, agentLabel, afterRun, queuedActions }: TranscriptProps) {
  return (
    <div className="tr-col">
      {entries.map((e, i) => (
        <Fragment key={e.id}>
          <ConversationEntry
            entry={e}
            agentLabel={agentLabel}
            queuedActions={queuedActions}
            labelled={isAgentKind(e) && !(i > 0 && isAgentKind(entries[i - 1]))}
          />
          {runEnd(entries, i, afterRun)}
        </Fragment>
      ))}
    </div>
  );
}

function ConversationEntry({
  entry: e,
  labelled,
  agentLabel,
  queuedActions,
}: {
  entry: Entry;
  labelled: boolean;
  agentLabel: string;
  queuedActions: TranscriptProps['queuedActions'];
}) {
  switch (e.kind) {
    case 'owner':
      return (
        <div className={cx('tr-you', e.queued && 'queued')}>
          {e.queued ? (
            <span className="tr-queued-note">
              {QUEUED_NOTE}
              {e.messageId && queuedActions ? queuedActions(e.messageId) : null}
            </span>
          ) : null}
          <span className="tr-body">{e.text}</span>
        </div>
      );
    case 'prompt':
      return <PromptSheet label={e.label} text={e.text} />;
    case 'text':
      return (
        <div className="tr-ai">
          {labelled ? <div className="tr-lbl">{agentLabel}</div> : null}
          <Markdown text={e.text} />
        </div>
      );
    case 'tool':
      return (
        <div className="tr-ai tr-ai-tool">
          {labelled ? <div className="tr-lbl">{agentLabel}</div> : null}
          <ToolRow entry={e} />
        </div>
      );
    case 'denial':
      return (
        <div className="tr-note tr-denial">
          <span className="tr-denied">DENIED</span>
          <pre>{e.text}</pre>
        </div>
      );
    case 'error':
      return (
        <div className="tr-note tr-error">
          <pre>{e.text}</pre>
        </div>
      );
    case 'system':
      return (
        <div className="tr-note tr-muted">
          <pre>{e.text}</pre>
        </div>
      );
    case 'stderr':
      return (
        <div className="tr-note tr-muted tr-stderr">
          <pre>{e.text}</pre>
        </div>
      );
    case 'divider':
      return (
        <div className="tr-divider" role="separator">
          <span>{e.text}</span>
        </div>
      );
  }
}

const PROMPT_PREVIEW_LINES = 6;

/** A task prompt can run to dozens of lines, so the conversation view shows a few and a toggle. */
function PromptSheet({ label, text }: { label: string; text: string }) {
  const [all, setAll] = useState(false);
  const { lines, clipped } = clipLines(text, PROMPT_PREVIEW_LINES, all);
  return (
    <div className="tr-you">
      <div className="tr-lbl">{label}</div>
      <span className="tr-body">{lines.join('\n')}</span>
      {clipped ? (
        <button type="button" className="link-button tr-more" onClick={() => setAll(!all)}>
          {all ? 'show less' : 'show more'}
        </button>
      ) : null}
    </div>
  );
}

function ToolRow({ entry }: { entry: ToolEntry }) {
  const [open, setOpen] = useState(false);
  const { tool } = entry;
  const hasDiff = tool.added !== undefined || tool.removed !== undefined;
  return (
    <div className={cx('tr-tool', open && 'open')}>
      <button type="button" className="tr-tool-row" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="tr-chev" aria-hidden="true">
          {open ? '▾' : '▸'}
        </span>
        <span className="tr-name">{tool.name}</span>
        <span className="tr-arg">{tool.arg}</span>
        {entry.resultError ? <span className="tr-denied tr-errtag">error</span> : null}
        {hasDiff ? (
          <span className="tr-diff">
            <span className="a">+{tool.added ?? 0}</span> <span className="d">−{tool.removed ?? 0}</span>
          </span>
        ) : null}
      </button>
      {open ? (
        <>
          {entry.input ? <pre>{entry.input}</pre> : null}
          {entry.result !== undefined ? (
            <pre className={cx(entry.resultError && 'tr-result-error')}>{entry.result}</pre>
          ) : (
            <pre className="tr-pending">No result yet.</pre>
          )}
        </>
      ) : null}
    </div>
  );
}

function TerminalView({ entries, afterRun, queuedActions }: TranscriptProps) {
  return (
    <div className="tr-term">
      {entries.map((e, i) => (
        <Fragment key={e.id}>
          <TerminalEntry entry={e} queuedActions={queuedActions} />
          {runEnd(entries, i, afterRun)}
        </Fragment>
      ))}
    </div>
  );
}

function TermLine({ time, marker, className, children }: { time?: string; marker: string; className: string; children: React.ReactNode }) {
  return (
    <div className={cx('tr-ln', className)}>
      <span className="tr-ts">{time ? clock(time) : ''}</span>
      <span className="tr-mk" aria-hidden="true">
        {marker}
      </span>
      <div className="tr-tx">{children}</div>
    </div>
  );
}

function TerminalEntry({ entry: e, queuedActions }: { entry: Entry; queuedActions: TranscriptProps['queuedActions'] }) {
  switch (e.kind) {
    case 'owner':
      return (
        <TermLine time={e.time} marker="›" className="k-user">
          {e.queued ? (
            <div className="tr-queued-note">
              {QUEUED_NOTE}
              {e.messageId && queuedActions ? queuedActions(e.messageId) : null}
            </div>
          ) : null}
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'prompt':
      return (
        <TermLine time={e.time} marker="›" className="k-user">
          <div className="tr-queued-note">{e.label}</div>
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'text':
      return (
        <TermLine time={e.time} marker="●" className="k-text">
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'tool':
      return <TerminalTool entry={e} />;
    case 'denial':
      return (
        <TermLine time={e.time} marker="●" className="k-denial">
          <span className="tr-denied">DENIED</span> <pre>{e.text}</pre>
        </TermLine>
      );
    case 'error':
      return (
        <TermLine time={e.time} marker="●" className="k-error">
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'system':
    case 'stderr':
      return (
        <TermLine time={e.time} marker="●" className="k-res">
          <pre>{e.text}</pre>
        </TermLine>
      );
    case 'divider':
      return (
        <div className="tr-divider" role="separator">
          <span>{e.text}</span>
        </div>
      );
  }
}

function TerminalTool({ entry }: { entry: ToolEntry }) {
  const [all, setAll] = useState(false);
  const { tool } = entry;
  const { lines: shown, clipped } =
    entry.result === undefined ? { lines: [], clipped: false } : clipLines(entry.result, RESULT_PREVIEW_LINES, all);
  const diff = tool.added !== undefined || tool.removed !== undefined ? `  +${tool.added ?? 0} −${tool.removed ?? 0}` : '';
  return (
    <>
      <TermLine time={entry.time} marker="●" className="k-tool">
        <pre>
          <span className="tr-name">{tool.name}</span>
          {tool.arg ? `  ${tool.arg}` : ''}
          {diff}
        </pre>
      </TermLine>
      {entry.result !== undefined ? (
        <TermLine marker="⎿" className={cx('k-res', entry.resultError && 'k-error')}>
          <pre>{shown.join('\n')}</pre>
          {clipped ? (
            <button type="button" className="link-button" onClick={() => setAll(!all)}>
              {all ? 'show less' : 'show more'}
            </button>
          ) : null}
        </TermLine>
      ) : null}
    </>
  );
}
