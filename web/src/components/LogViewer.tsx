import { useEffect, useRef, useState } from 'react';
import { getRunLogs } from '../api';
import { subscribe, useLiveStatus } from '../events';
import { formatClock, cx } from '../lib/format';
import { maxSeq, mergeLogLines } from '../lib/logs';
import { parseTime } from '../lib/time';
import type { LogKind, LogLine } from '../types';
import { ErrorLine } from './ErrorLine';

const PAGE = 1_000;
const POLL_MS = 3_000;
const TOOL_RESULT_PREVIEW_LINES = 3;

/** Reads every page of a run's log. */
async function fetchAllLogs(runId: string): Promise<LogLine[]> {
  const out: LogLine[] = [];
  let after = 0;
  for (;;) {
    const page = await getRunLogs(runId, { after, limit: PAGE });
    out.push(...page);
    if (page.length < PAGE) return out;
    after = maxSeq(page);
  }
}

/**
 * A run's output: the stored log first, then live run.log events for this run. Duplicates are
 * dropped by seq. When the run is active but the stream is down, the log is polled every 3s.
 */
export function LogViewer({ runId, active }: { runId: string; active: boolean }) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [follow, setFollow] = useState(true);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(() => new Set());
  const live = useLiveStatus() === 'live';
  const linesRef = useRef<LogLine[]>([]);
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    linesRef.current = lines;
  }, [lines]);

  // Initial load for this run.
  useEffect(() => {
    let cancelled = false;
    setLines([]);
    setLoaded(false);
    setError(null);
    fetchAllLogs(runId)
      .then((all) => {
        if (cancelled) return;
        setLines((prev) => mergeLogLines(prev, all));
        setLoaded(true);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err);
      });
    return () => {
      cancelled = true;
    };
  }, [runId]);

  // Live lines from the event stream.
  useEffect(
    () =>
      subscribe((ev) => {
        if (ev.type !== 'run.log' || !Array.isArray(ev.data)) return;
        const incoming = (ev.data as LogLine[]).filter((line) => line.run_id === runId);
        if (incoming.length > 0) setLines((prev) => mergeLogLines(prev, incoming));
      }),
    [runId],
  );

  const catchUp = async () => {
    try {
      const more = await getRunLogs(runId, { after: maxSeq(linesRef.current), limit: PAGE });
      if (more.length > 0) setLines((prev) => mergeLogLines(prev, more));
    } catch {
      // The next poll or reconnect tries again.
    }
  };

  // Poll while the run is active and the stream is not delivering.
  useEffect(() => {
    if (!loaded || !active || live) return;
    const timer = window.setInterval(() => void catchUp(), POLL_MS);
    return () => window.clearInterval(timer);
    // catchUp only reads runId and refs, so the effect's own deps are enough.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, active, live, runId]);

  // Lines emitted while the stream was down are never replayed, so fetch them on reconnect.
  useEffect(() => {
    if (loaded && live) void catchUp();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, live, runId]);

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
