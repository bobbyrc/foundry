import type { LogLine } from '../types';

/** Merges log lines by seq: duplicates keep the first copy, and the result is sorted by seq. */
export function mergeLogLines(existing: readonly LogLine[], incoming: readonly LogLine[]): LogLine[] {
  const bySeq = new Map<number, LogLine>();
  for (const line of existing) bySeq.set(line.seq, line);
  for (const line of incoming) {
    if (!bySeq.has(line.seq)) bySeq.set(line.seq, line);
  }
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq);
}

/** Highest seq in the list, or 0. Used as the `after` cursor for catch-up requests. */
export function maxSeq(lines: readonly LogLine[]): number {
  let max = 0;
  for (const line of lines) if (line.seq > max) max = line.seq;
  return max;
}
