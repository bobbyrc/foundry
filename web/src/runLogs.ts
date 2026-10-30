import { useEffect, useRef, useState } from 'react';
import { useQueries } from '@tanstack/react-query';
import { getRunLogs } from './api';
import { subscribe, useLiveStatus, watch } from './events';
import { keys } from './keys';
import { maxSeq, mergeLogLines } from './lib/logs';
import type { LogLine } from './types';

const PAGE = 1_000;
const POLL_MS = 3_000;

/** Reads every page of a run's log. */
export async function fetchAllLogs(runId: string): Promise<LogLine[]> {
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
export function useRunLogLines(runId: string | undefined, active: boolean): { lines: LogLine[]; loading: boolean; error?: Error } {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [loaded, setLoaded] = useState(false);
  // The run id the server has confirmed a watch for. Holding the id, not a flag, means a value left
  // over from the previous run never matches the new one.
  const [subscribedRunId, setSubscribedRunId] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const live = useLiveStatus() === 'live';
  const linesRef = useRef<LogLine[]>([]);

  useEffect(() => {
    linesRef.current = lines;
  }, [lines]);

  // Initial load for this run.
  useEffect(() => {
    let cancelled = false;
    setLines([]);
    setLoaded(false);
    setError(null);
    if (!runId) return;
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
        if (!runId) return;
        const incoming = (ev.data as LogLine[]).filter((line) => line.run_id === runId);
        if (incoming.length > 0) setLines((prev) => mergeLogLines(prev, incoming));
      }),
    [runId],
  );

  const catchUp = async () => {
    if (!runId) return;
    try {
      const more = await getRunLogs(runId, { after: maxSeq(linesRef.current), limit: PAGE });
      if (more.length > 0) setLines((prev) => mergeLogLines(prev, more));
    } catch {
      // The next poll or reconnect tries again.
    }
  };

  // The server sends a run's log lines only to a socket that asked for them. Hold the watch for this
  // run and note when the server confirms it.
  useEffect(() => {
    setSubscribedRunId(null);
    if (!runId) return;
    const w = watch({ run_id: runId });
    let cancelled = false;
    void w.ready.then(() => {
      if (!cancelled) setSubscribedRunId(runId);
    });
    return () => {
      cancelled = true;
      w.release();
    };
  }, [runId]);

  // Lines emitted before the watch took effect are never replayed, so fetch them. This waits for
  // the initial load and the confirmation: an earlier catch-up would read a log that is not loaded
  // yet and pick up only the first page.
  useEffect(() => {
    if (loaded && subscribedRunId === runId) void catchUp();
    // catchUp only reads runId and refs, so the effect's own deps are enough.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, subscribedRunId, runId]);

  // Poll while the run is active and the stream is not delivering.
  useEffect(() => {
    if (!runId || !loaded || !active || live) return;
    const timer = window.setInterval(() => void catchUp(), POLL_MS);
    return () => window.clearInterval(timer);
    // catchUp only reads runId and refs, so the effect's own deps are enough.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, active, live, runId]);

  // Lines emitted while the stream was down are never replayed, so fetch them on reconnect.
  useEffect(() => {
    if (runId && loaded && live) void catchUp();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, live, runId]);

  return { lines, loading: !!runId && !loaded && !error, error: error ? (error as Error) : undefined };
}

/**
 * The logs of several runs by run id. Finished runs are read once and cached (a run's end
 * refreshes them, see events.ts); the live run, if any, streams through `useRunLogLines`.
 */
export function useRunLogsMap(runIds: string[], liveRunId?: string): Record<string, LogLine[]> {
  const finished = runIds.filter((id) => id !== liveRunId);
  const results = useQueries({
    queries: finished.map((id) => ({
      queryKey: keys.runLogs(id),
      queryFn: () => fetchAllLogs(id),
      staleTime: Infinity,
    })),
  });
  const live = useRunLogLines(liveRunId, true);

  // When the live run ends, its lines stay on screen until the finished-run read arrives, so the
  // transcript does not blink empty in between.
  const lastLive = useRef<Record<string, LogLine[]>>({});
  if (liveRunId && live.lines.length > 0) lastLive.current[liveRunId] = live.lines;

  const out: Record<string, LogLine[]> = {};
  finished.forEach((id, i) => {
    const data = results[i]?.data;
    const kept = lastLive.current[id];
    if (data && (!kept || data.length >= kept.length)) out[id] = data;
    else if (kept) out[id] = kept;
  });
  if (liveRunId) out[liveRunId] = live.lines;
  return out;
}
