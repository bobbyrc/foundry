// Live runs on the Overview are bars on one shared time axis: a bar's length is the run's real
// elapsed time, so a long or stalling run is visibly long. The part since the last output is drawn
// apart, so a run that has gone quiet shows it.

const MINUTE = 60_000;
/** Axis lengths to choose from, in minutes. The axis is the smallest that fits the longest run. */
const SCALES = [10, 20, 30, 60, 120, 240, 480];

export function axisMinutes(longestElapsedMs: number): number {
  const minutes = longestElapsedMs / MINUTE;
  return SCALES.find((s) => minutes <= s) ?? Math.ceil(minutes / 60) * 60;
}

/** Tick labels for an axis: 0 and three more evenly spaced marks. */
export function axisTicks(scaleMinutes: number): string[] {
  return [0, 1, 2, 3].map((i) => {
    const m = (scaleMinutes / 3) * i;
    return m >= 60 && m % 60 === 0 ? `${m / 60}h` : `${Math.round(m)}m`;
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
