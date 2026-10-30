import { useEffect, useRef, useState } from 'react';
import { useRunLogLines } from '../runLogs';
import { formatClock, cx } from '../lib/format';
import { parseTime } from '../lib/time';
import type { LogKind, LogLine } from '../types';
import { ErrorLine } from './ErrorLine';

const TOOL_RESULT_PREVIEW_LINES = 3;

/** A run's output; the paging, live stream and polling live in `useRunLogLines`. */
export function LogViewer({ runId, active }: { runId: string; active: boolean }) {
  const { lines, loading, error } = useRunLogLines(runId, active);
  const loaded = !loading && !error;
  const [follow, setFollow] = useState(true);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(() => new Set());
  const bodyRef = useRef<HTMLDivElement>(null);

  // Follow: keep the view pinned to the newest line.
  useEffect(() => {
    const el = bodyRef.current;
    if (follow && el) el.scrollTop = el.scrollHeight;
  }, [lines, follow]);

  const toggleExpanded = (seq: number) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(seq)) next.delete(seq);
      else next.add(seq);
      return next;
    });

  return (
    <section className="log-panel" aria-label="Run log">
      <div className="log-toolbar">
        <h2>Log</h2>
        <div className="log-toolbar-end">
          <span className="log-count muted">
            {lines.length} {lines.length === 1 ? 'line' : 'lines'}
          </span>
          <label className="toggle">
            <input type="checkbox" checked={follow} onChange={(event) => setFollow(event.target.checked)} />
            Follow
          </label>
        </div>
      </div>
      {error ? <ErrorLine error={error} /> : null}
      {!error && !loaded ? <p className="muted">Loading log…</p> : null}
      <div className="log" ref={bodyRef} role="log" aria-label={`Output of run ${runId}`}>
        {loaded && lines.length === 0 ? (
          <p className="log-empty muted">
            {active ? 'No output yet. Lines appear here as the agent works.' : 'This run left no output.'}
          </p>
        ) : null}
        {lines.map((line) => (
          <LogRow key={line.seq} line={line} expanded={expanded.has(line.seq)} onToggle={() => toggleExpanded(line.seq)} />
        ))}
      </div>
    </section>
  );
}

/** The marker on a tool_use line: a small drawn chevron, in the run colour. */
function Chevron() {
  return (
    <svg className="log-chevron" viewBox="0 0 10 10" width="10" height="10" aria-hidden="true" focusable="false">
      <polyline points="3,2 7,5 3,8" fill="none" stroke="currentColor" strokeWidth="1.6" />
    </svg>
  );
}

function LogRow({ line, expanded, onToggle }: { line: LogLine; expanded: boolean; onToggle: () => void }) {
  const kind: LogKind = line.kind;
  const textLines = line.text.split('\n');
  const collapsible = kind === 'tool_result' && textLines.length > TOOL_RESULT_PREVIEW_LINES;
  const shown = collapsible && !expanded ? textLines.slice(0, TOOL_RESULT_PREVIEW_LINES).join('\n') : line.text;
  const time = parseTime(line.time);
  const text = <pre>{shown}</pre>;

  return (
    <div className={cx('log-line', `kind-${kind}`)}>
      <span className="gutter">{time ? formatClock(time) : ''}</span>
      <div className="log-body">
        {kind === 'denial' ? <span className="log-denied">DENIED</span> : null}
        {kind === 'prompt' ? <span className="log-label">Prompt</span> : null}
        {kind === 'owner' ? <span className="log-label">You</span> : null}
        {kind === 'tool_use' ? (
          <div className="log-tool">
            <Chevron />
            {text}
          </div>
        ) : (
          text
        )}
        {collapsible ? (
          <button type="button" className="link-button" onClick={onToggle}>
            {expanded ? 'show less' : 'show more'}
          </button>
        ) : null}
      </div>
    </div>
  );
}
