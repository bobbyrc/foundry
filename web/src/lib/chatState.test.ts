import { describe, expect, it } from 'vitest';
import type { AgentConversation } from '../types';
import { agentLamp, composerState, taskLamp } from './chatState';

const base = { canSend: true, viewer: false, working: false, queued: 0 };

describe('composerState', () => {
  it('lets an operator message the orchestrator', () => {
    expect(composerState(base)).toEqual({ enabled: true, placeholder: 'Message the orchestrator…' });
  });
  it('names the role for a task agent', () => {
    expect(composerState({ ...base, role: 'implementer' })).toMatchObject({
      enabled: true,
      placeholder: 'Message the implementer…',
    });
  });
  it('stays enabled while the agent works and while messages are queued', () => {
    expect(composerState({ ...base, working: true, queued: 2 })).toEqual({
      enabled: true,
      placeholder: 'Message the orchestrator…',
    });
  });
  it('tells a viewer they can only read', () => {
    expect(composerState({ ...base, viewer: true })).toEqual({
      enabled: false,
      placeholder: 'Message the orchestrator…',
      note: 'Viewers can read conversations but not send messages.',
    });
  });
  it('gives the reason when the conversation cannot take messages', () => {
    expect(composerState({ ...base, canSend: false, reason: 'closed by owner: done' })).toMatchObject({
      enabled: false,
      note: 'closed by owner: done',
    });
    expect(composerState({ ...base, canSend: false })).toMatchObject({
      enabled: false,
      note: 'This conversation cannot take messages.',
    });
  });
  it('puts the viewer line ahead of a closed reason', () => {
    expect(composerState({ ...base, viewer: true, canSend: false, reason: 'x' }).note).toBe(
      'Viewers can read conversations but not send messages.',
    );
  });
});

const agent = (p: Partial<AgentConversation>): AgentConversation => ({
  task_id: 't',
  title: 'T',
  project: 'p',
  role: 'implementer',
  task_status: 'review',
  live: false,
  closed: false,
  updated_at: '2026-01-01T00:00:00Z',
  ...p,
});

describe('agentLamp', () => {
  it('blinks for a live agent', () => {
    expect(agentLamp(agent({ live: true, task_status: 'running' }))).toEqual({ lamp: 'run', label: 'working' });
  });
  it('is dim when closed', () => {
    expect(agentLamp(agent({ closed: true }))).toEqual({ lamp: 'done', label: 'closed' });
  });
  it('is red for a blocked task', () => {
    expect(agentLamp(agent({ task_status: 'blocked' }))).toEqual({ lamp: 'stop', label: 'blocked' });
  });
  it('is done otherwise, labelled with the task status', () => {
    expect(agentLamp(agent({ task_status: 'awaiting_approval' }))).toEqual({
      lamp: 'done',
      label: 'awaiting approval',
    });
  });
});

describe('taskLamp', () => {
  it('maps a task status to a lamp and a label', () => {
    expect(taskLamp('running')).toEqual({ lamp: 'run', label: 'running' });
    expect(taskLamp('blocked')).toEqual({ lamp: 'stop', label: 'blocked' });
    expect(taskLamp('failed')).toEqual({ lamp: 'stop', label: 'failed' });
    expect(taskLamp('awaiting_approval')).toEqual({ lamp: 'call', label: 'awaiting approval' });
    expect(taskLamp('done')).toEqual({ lamp: 'done', label: 'done' });
  });
});
