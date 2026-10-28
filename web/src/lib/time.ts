const SECOND = 1_000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** Parses an API timestamp. Returns null for empty values and Go's zero time ("never"). */
export function parseTime(value: string | Date | null | undefined): Date | null {
  if (value === null || value === undefined || value === '') return null;
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  // Go's zero time is 0001-01-01T00:00:00Z; the API uses it for "never".
  if (date.getUTCFullYear() <= 1) return null;
  return date;
}

/** "just now", "12s ago", "3m ago", "2h ago", "4d ago", then a calendar date. */
export function formatRelative(date: Date, now: number): string {
  const diff = now - date.getTime();
  if (diff < -5 * SECOND) return `in ${formatDuration(-diff)}`;
  if (diff < 5 * SECOND) return 'just now';
  if (diff < MINUTE) return `${Math.floor(diff / SECOND)}s ago`;
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)}m ago`;
  if (diff < DAY) return `${Math.floor(diff / HOUR)}h ago`;
  if (diff < 30 * DAY) return `${Math.floor(diff / DAY)}d ago`;
  return date.toLocaleDateString();
}

/** "12s", "3m 04s", "1h 05m". Negative input is treated as zero. */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / SECOND));
  if (total < 60) return `${total}s`;
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  if (minutes < 60) return `${minutes}m ${String(seconds).padStart(2, '0')}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${String(minutes % 60).padStart(2, '0')}m`;
}

/** Time from start to end, or to now when the span is still open. */
export function elapsedMs(start: Date, end: Date | null, now: number): number {
  return (end ?? new Date(now)).getTime() - start.getTime();
}
