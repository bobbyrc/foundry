// Live runs on the Overview are bars on one shared time axis: a bar's length is the run's real
// elapsed time, so a long or stalling run is visibly long. The part since the last output is drawn
// apart, so a run that has gone quiet shows it.

const MINUTE = 60_000;
/** Axis lengths to choose from, in minutes. The axis is the smallest that fits the longest run. */
const SCALES = [20, 40, 60, 120, 240, 480];

export function axisMinutes(longestElapsedMs: number): number {
  const minutes = longestElapsedMs / MINUTE;
  return SCALES.find((s) => minutes <= s) ?? Math.ceil(minutes / 60) * 60;
}

/** Tick labels for an axis: 0 and four quarter marks. Under an hour: "15m"; whole hours: "2h"; else "1h30m". */
export function axisTicks(scaleMinutes: number): string[] {
  return [0, 1, 2, 3, 4].map((i) => {
    const m = Math.round((scaleMinutes / 4) * i);
    if (m < 60) return `${m}m`;
    const h = Math.floor(m / 60);
    const rest = m % 60;
    return rest === 0 ? `${h}h` : `${h}h${rest}m`;
  });
}

export interface RunBar {
  /** Percent of the axis from the start to the last output (or to now when output is fresh). */
  flow: number;
  /** Percent of the axis from the last output to now; 0 unless the run has been quiet a while. */
  quiet: number;
}

/**
 * started and lastOutput are epoch milliseconds; lastOutput is null before the first output.
 * quietAfterMs is how long without output before the quiet part is drawn.
 */
export function runBar(started: number, lastOutput: number | null, now: number, scaleMinutes: number, quietAfterMs: number): RunBar {
  const scale = scaleMinutes * MINUTE;
  const clamp = (ms: number) => Math.max(0, Math.min(100, (ms / scale) * 100));
  const total = clamp(now - started);
  const last = lastOutput ?? started;
  if (now - last < quietAfterMs) return { flow: total, quiet: 0 };
  const flow = clamp(last - started);
  return { flow, quiet: Math.max(0, total - flow) };
}
