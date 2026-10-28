import { describe, expect, it } from 'vitest';
import type { TaskStatus } from '../types';
import { taskLine, type Station, type StationState } from './taskLine';

/** The state of each station, in order, as a compact string for easy comparison. */
const states = (stations: Station[]) => stations.map((s) => s.state);

describe('taskLine', () => {
  it('lists seven stations with the labels in order', () => {
    expect(taskLine({ status: 'queued', hasPR: false, approvalCount: 0 }).map((s) => s.label)).toEqual([
      'Queued',
      'Running',
      'Review',
      'Approval',
      'Merged',
      'Deploying',
      'Done',
    ]);
  });

  it('marks a queued task current, with the rest ahead', () => {
    const line = taskLine({ status: 'queued', hasPR: false, approvalCount: 0 });
    expect(states(line)).toEqual(['current', 'ahead', 'ahead', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(line[0].tone).toBe('off');
  });

  it('marks a running task current with the run tone', () => {
    const line = taskLine({ status: 'running', hasPR: false, approvalCount: 0 });
    expect(states(line)).toEqual(['passed', 'current', 'ahead', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(line[1].tone).toBe('run');
  });

  it('marks review current with the run tone and passes queued and running', () => {
    const line = taskLine({ status: 'review', hasPR: true, approvalCount: 0 });
    expect(states(line)).toEqual(['passed', 'passed', 'current', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(line[2].tone).toBe('run');
  });

  it('marks awaiting_approval current with the call tone', () => {
    const line = taskLine({ status: 'awaiting_approval', hasPR: true, approvalCount: 1 });
    expect(states(line)).toEqual(['passed', 'passed', 'passed', 'current', 'ahead', 'ahead', 'ahead']);
    expect(line[3].tone).toBe('call');
  });

  it('marks merged current with the run tone', () => {
    const line = taskLine({ status: 'merged', hasPR: true, approvalCount: 1 });
    expect(states(line)).toEqual(['passed', 'passed', 'passed', 'passed', 'current', 'ahead', 'ahead']);
    expect(line[4].tone).toBe('run');
  });

  it('marks deploying current with the run tone', () => {
    const line = taskLine({ status: 'deploying', hasPR: true, approvalCount: 1 });
    expect(states(line)).toEqual(['passed', 'passed', 'passed', 'passed', 'passed', 'current', 'ahead']);
    expect(line[5].tone).toBe('run');
  });

  it('passes every station for a done task, with no current station and no tone', () => {
    const line = taskLine({ status: 'done', hasPR: true, approvalCount: 1 });
    expect(states(line)).toEqual(['passed', 'passed', 'passed', 'passed', 'passed', 'passed', 'passed']);
    expect(line.some((s) => s.tone !== undefined)).toBe(false);
  });

  it('skips approval when nothing was approved, because it merged on its own', () => {
    expect(states(taskLine({ status: 'done', hasPR: true, approvalCount: 0 }))[3]).toBe('skipped');
    expect(states(taskLine({ status: 'merged', hasPR: true, approvalCount: 0 }))[3]).toBe('skipped');
    expect(states(taskLine({ status: 'done', hasPR: true, approvalCount: 2 }))[3]).toBe('passed');
  });

  it('skips review for a done task with no PR, answered without code', () => {
    expect(states(taskLine({ status: 'done', hasPR: false, approvalCount: 0 }))[2]).toBe('skipped');
    expect(states(taskLine({ status: 'done', hasPR: true, approvalCount: 0 }))[2]).toBe('passed');
  });

  it('keeps review passed for a merged task with no PR', () => {
    expect(states(taskLine({ status: 'merged', hasPR: false, approvalCount: 1 }))[2]).toBe('passed');
  });

  it.each<[TaskStatus, boolean, number]>([
    ['blocked', true, 0],
    ['failed', true, 1],
  ])('stops %s at review when there is a PR', (status, hasPR, approvalCount) => {
    const line = taskLine({ status, hasPR, approvalCount });
    expect(states(line)).toEqual(['passed', 'passed', 'stopped', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(line[2].tone).toBe('stop');
  });

  it.each<[TaskStatus]>([['blocked'], ['failed']])('stops %s at running when there is no PR', (status) => {
    const line = taskLine({ status, hasPR: false, approvalCount: 0 });
    expect(states(line)).toEqual(['passed', 'stopped', 'ahead', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(line[1].tone).toBe('stop');
  });

  it('stops cancelled with the off tone, at the same place as a blocked task', () => {
    const withPR = taskLine({ status: 'cancelled', hasPR: true, approvalCount: 0 });
    expect(states(withPR)).toEqual(['passed', 'passed', 'stopped', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(withPR[2].tone).toBe('off');

    const withoutPR = taskLine({ status: 'cancelled', hasPR: false, approvalCount: 0 });
    expect(states(withoutPR)).toEqual(['passed', 'stopped', 'ahead', 'ahead', 'ahead', 'ahead', 'ahead']);
    expect(withoutPR[1].tone).toBe('off');
  });

  it('never marks a stopped task current', () => {
    for (const status of ['blocked', 'failed', 'cancelled'] as TaskStatus[]) {
      expect(states(taskLine({ status, hasPR: true, approvalCount: 0 })).includes('current')).toBe(false);
    }
  });

  it('keeps the stopped station out of the ahead stations and skips nothing before a stop', () => {
    // A blocked task that never reached approval: approval is after the stop, so it is ahead.
    const line = taskLine({ status: 'blocked', hasPR: true, approvalCount: 0 });
    expect(line[3]).toMatchObject({ key: 'approval', state: 'ahead' });
  });

  it('gives every station a state from the known set', () => {
    const known: StationState[] = ['passed', 'current', 'stopped', 'skipped', 'ahead'];
    const all: TaskStatus[] = ['queued', 'running', 'review', 'awaiting_approval', 'merged', 'deploying', 'done', 'blocked', 'failed', 'cancelled'];
    for (const status of all) {
      for (const hasPR of [true, false]) {
        for (const approvalCount of [0, 1]) {
          for (const s of taskLine({ status, hasPR, approvalCount })) expect(known).toContain(s.state);
        }
      }
    }
  });
});
