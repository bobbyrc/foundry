import { describe, expect, it } from 'vitest';
import { axisMinutes, axisTicks, runBar } from './runbar';

const MIN = 60_000;

describe('axisMinutes', () => {
  it('picks the smallest axis that fits the longest run', () => {
    expect(axisMinutes(0)).toBe(10);
    expect(axisMinutes(9 * MIN)).toBe(10);
    expect(axisMinutes(11 * MIN)).toBe(20);
    expect(axisMinutes(45 * MIN)).toBe(60);
    expect(axisMinutes(10 * 60 * MIN)).toBe(600);
  });
});

describe('axisTicks', () => {
  it('labels four evenly spaced marks, switching to hours on whole hours', () => {
    expect(axisTicks(30)).toEqual(['0m', '10m', '20m', '30m']);
    expect(axisTicks(180)).toEqual(['0m', '1h', '2h', '3h']);
  });
});

describe('runBar', () => {
  const now = 100 * MIN;

  it('draws one flowing bar while output is fresh', () => {
    expect(runBar(now - 15 * MIN, now - 10_000, now, 30, 5 * MIN)).toEqual({ flow: 50, quiet: 0 });
  });

  it('splits off the quiet part once the run has been silent past the threshold', () => {
    expect(runBar(now - 15 * MIN, now - 6 * MIN, now, 30, 5 * MIN)).toEqual({ flow: 30, quiet: 20 });
  });

  it('counts a run with no output yet as quiet from its start', () => {
    expect(runBar(now - 6 * MIN, null, now, 30, 5 * MIN)).toEqual({ flow: 0, quiet: 20 });
  });

  it('never draws past the axis', () => {
    const bar = runBar(now - 90 * MIN, now, now, 30, 5 * MIN);
    expect(bar.flow + bar.quiet).toBe(100);
  });
});
