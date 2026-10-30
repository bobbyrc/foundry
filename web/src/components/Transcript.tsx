import { useState } from 'react';
import { cx, formatClock } from '../lib/format';
import { parseTime } from '../lib/time';
import type { Entry } from '../lib/transcript';
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

/** A run of agent entries, or an owner's turn, laid out as a conversation or as terminal lines. */
export function Transcript({ entries, view }: { entries: Entry[]; view: TranscriptView }) {
  return view === 'terminal' ? <TerminalView entries={entries} /> : <ConversationView entries={entries} />;
}

const isAgentKind = (e: Entry) => e.kind === 'text' || e.kind === 'tool';

function ConversationView({ entries }: { entries: Entry[] }) {
  return (
    <div className="tr-col">
      {entries.map((e, i) => (
        <ConversationEntry key={i} entry={e} labelled={isAgentKind(e) && !(i > 0 && isAgentKind(entries[i - 1]))} />
      ))}
    </div>
  );
}

function ConversationEntry({ entry: e, labelled }: { entry: Entry; labelled: boolean }) {
  switch (e.kind) {
    case 'owner':
      return (
        <div className={cx('tr-you', e.queued && 'queued')}>
          {e.queued ? <span className="tr-queued-note">{QUEUED_NOTE}</span> : null}
          <span className="tr-body">{e.text}</span>
        </div>
      );
    case 'prompt':
      return (
        <div className="tr-you">
          <div className="tr-lbl">{e.label}</div>
          <span className="tr-body">{e.text}</span>
        </div>
      );
    case 'text':
      return (
        <div className="tr-ai">
          {labelled ? <div className="tr-lbl">Agent</div> : null}
          <Markdown text={e.text} />
        </div>
      );
    case 'tool':
      return (
        <div className="tr-ai tr-ai-tool">
          {labelled ? <div className="tr-lbl">Agent</div> : null}
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

function TerminalView({ entries }: { entries: Entry[] }) {
  return (
    <div className="tr-term">
      {entries.map((e, i) => (
        <TerminalEntry key={i} entry={e} />
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

function TerminalEntry({ entry: e }: { entry: Entry }) {
  switch (e.kind) {
    case 'owner':
      return (
        <TermLine time={e.time} marker="›" className="k-user">
          {e.queued ? <div className="tr-queued-note">{QUEUED_NOTE}</div> : null}
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
  const resultLines = entry.result === undefined ? [] : entry.result.split('\n');
  const clipped = resultLines.length > RESULT_PREVIEW_LINES;
  const shown = clipped && !all ? resultLines.slice(0, RESULT_PREVIEW_LINES) : resultLines;
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
