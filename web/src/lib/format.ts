/** Joins class names, skipping falsy parts. */
export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}

export const shortId = (id: string): string => id.slice(0, 8);

export const shortSha = (sha: string): string => sha.slice(0, 7);

/** "$0.42"; amounts under a cent show as "<$0.01". */
export function formatCost(usd: number | null | undefined): string {
  if (usd === null || usd === undefined || Number.isNaN(usd)) return '—';
  if (usd > 0 && usd < 0.01) return '<$0.01';
  return `$${usd.toFixed(2)}`;
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

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
