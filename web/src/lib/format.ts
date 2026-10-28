/** Joins class names, skipping falsy parts. */
export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}

/** The last 8 characters of an ID. UUIDv7 starts with a timestamp, so its head repeats across tasks. */
export const shortId = (id: string): string => id.slice(-8);

/** The first 7 characters of a git SHA, which is what git log and GitHub show. */
export const shortSha = (sha: string): string => sha.slice(0, 7);

/** "$0.42"; amounts under a cent show as "<$0.01". */
export function formatCost(usd: number | null | undefined, estimated = false): string {
  if (usd === null || usd === undefined || Number.isNaN(usd)) return '—';
  const approx = estimated ? '~' : '';
  if (usd > 0 && usd < 0.01) return `${approx}<$0.01`;
  return `${approx}$${usd.toFixed(2)}`;
}

/** "950", "12.3k", "15k", "1.5M". */
export function formatTokens(n: number): string {
  if (n < 1_000) return String(n);
  if (n < 1_000_000) return `${trimZero((n / 1_000).toFixed(1))}k`;
  return `${trimZero((n / 1_000_000).toFixed(1))}M`;
}

const trimZero = (s: string): string => s.replace(/\.0$/, '');

/** Only http and https links are rendered as anchors; anything else (javascript:, data:) is not. */
export function safeHref(url: string | null | undefined): string | undefined {
  if (!url) return undefined;
  return /^https?:\/\//i.test(url) ? url : undefined;
}

export function formatClock(value: Date): string {
  return value.toLocaleTimeString([], { hour12: false });
}

export const humanize = (value: string): string => value.replace(/_/g, ' ');

/**
 * A Go duration string with its zero parts dropped: "1m0s" -> "1m", "1h30m0s" -> "1h30m".
 * "0s" stays "0s". Anything that is not h/m/s parts is returned unchanged.
 */
export function formatGoDuration(s: string): string {
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/.exec(s);
  if (!match || s === '') return s;
  const [, hours, minutes, seconds] = match;
  const parts: string[] = [];
  if (hours && Number(hours) > 0) parts.push(`${Number(hours)}h`);
  if (minutes && Number(minutes) > 0) parts.push(`${Number(minutes)}m`);
  if (seconds && Number(seconds) > 0) parts.push(`${Number(seconds)}s`);
  return parts.length > 0 ? parts.join('') : '0s';
}

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
