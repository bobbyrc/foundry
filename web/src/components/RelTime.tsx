import { formatRelative, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';

/** A relative time ("3m ago") with the full timestamp on hover. */
export function RelTime({ value }: { value: string | null | undefined }) {
  const now = useNow();
  const date = parseTime(value);
  if (!date) return <span className="muted">—</span>;
  return (
    <time dateTime={date.toISOString()} title={date.toLocaleString()}>
      {formatRelative(date, now)}
    </time>
  );
}
