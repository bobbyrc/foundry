import { describe, expect, it } from 'vitest';
import type { AgentView } from '../types';
import {
  blankForm,
  formFromAgent,
  removeChanges,
  resetChanges,
  restoreChanges,
  saveChanges,
  type AgentForm,
} from './agentChanges';

function view(overrides: Partial<AgentView>): AgentView {
  return {
    name: 'docs',
    origin: 'custom',
    disabled: false,
    harness: 'claude',
    harness_type: 'claude',
    model: '',
    prompt: '',
    prompt_text: 'Write docs.',
    prompt_updates: false,
    description: 'Writes docs.',
    args: [],
    kinds: [],
    uses: [],
    ...overrides,
  };
}

function form(overrides: Partial<AgentForm>): AgentForm {
  return {
    name: 'docs',
    description: '',
    harness: 'claude',
    model: '',
    args: [],
    promptMode: 'text',
    promptRef: '',
    promptText: 'Write docs.',
    ...overrides,
  };
}

describe('blankForm', () => {
  it('starts every field empty, in text mode, with no args', () => {
    expect(blankForm()).toEqual({
      name: '',
      description: '',
      harness: '',
      model: '',
      args: [],
      promptMode: 'text',
      promptRef: '',
      promptText: '',
    });
  });
});

describe('removeChanges', () => {
  it('deletes a custom agent, since nothing would bring it back', () => {
    const files = { docs: { harness: 'claude' }, other: { harness: 'claude' } };
    expect(removeChanges(view({ name: 'docs', origin: 'custom' }), files)).toEqual([
      { op: 'delete', path: ['agents', 'docs'] },
    ]);
  });

  it('deletes the whole agents mapping when a custom agent is the only entry', () => {
    expect(removeChanges(view({ name: 'docs', origin: 'custom' }), { docs: { harness: 'claude' } })).toEqual([
      { op: 'delete', path: ['agents'] },
    ]);
  });

  it('disables a built-in agent that has no file entry', () => {
    expect(
      removeChanges(view({ name: 'improver', origin: 'builtin', prompt: 'builtin:improver' }), {
        docs: { harness: 'claude' },
      }),
    ).toEqual([{ op: 'set', path: ['agents', 'improver'], value: { disabled: true } }]);
  });

  it('sets disabled on an edited agent whose entry is present', () => {
    expect(
      removeChanges(view({ name: 'reviewer', origin: 'edited', prompt: 'builtin:reviewer' }), {
        reviewer: { harness: 'claude', prompt: 'builtin:reviewer' },
      }),
    ).toEqual([{ op: 'set', path: ['agents', 'reviewer', 'disabled'], value: true }]);
  });
});

describe('restoreChanges', () => {
  it('deletes the whole entry when it only disables a built-in and other entries remain', () => {
    expect(restoreChanges('improver', { improver: { disabled: true }, docs: { harness: 'claude' } })).toEqual([
      { op: 'delete', path: ['agents', 'improver'] },
    ]);
  });

  it('deletes the whole agents mapping when a disabled-only entry is the only one', () => {
    expect(restoreChanges('improver', { improver: { disabled: true } })).toEqual([
      { op: 'delete', path: ['agents'] },
    ]);
  });

  it('removes only the disabled flag when the entry sets other fields', () => {
    expect(restoreChanges('reviewer', { reviewer: { harness: 'claude', disabled: true } })).toEqual([
      { op: 'delete', path: ['agents', 'reviewer', 'disabled'] },
    ]);
  });

  it('changes nothing when the file has no entry for the agent', () => {
    expect(restoreChanges('improver', { docs: { harness: 'claude' } })).toEqual([]);
  });
});

describe('resetChanges', () => {
  it('deletes the entry so the built-in comes back, keeping other entries', () => {
    expect(resetChanges('reviewer', { reviewer: { harness: 'claude' }, docs: { harness: 'claude' } })).toEqual([
      { op: 'delete', path: ['agents', 'reviewer'] },
    ]);
  });

  it('deletes the whole agents mapping when the entry is the only one', () => {
    expect(resetChanges('reviewer', { reviewer: { harness: 'claude' } })).toEqual([{ op: 'delete', path: ['agents'] }]);
  });
});

describe('saveChanges', () => {
  it('writes an inline prompt as prompt_text and leaves out empty optional fields', () => {
    const changes = saveChanges(form({ promptMode: 'text', promptText: 'Triage bugs.', model: '', description: '', args: [] }));
    expect(changes).toHaveLength(1);
    expect(changes[0].op).toBe('set');
    expect(changes[0].path).toEqual(['agents', 'docs']);
    const value = changes[0].value as Record<string, unknown>;
    expect(value).toEqual({ harness: 'claude', prompt_text: 'Triage bugs.' });
    expect(value).not.toHaveProperty('prompt');
    expect(value).not.toHaveProperty('model');
    expect(value).not.toHaveProperty('description');
    expect(value).not.toHaveProperty('args');
  });

  it('writes a reference as prompt and drops prompt_text', () => {
    const changes = saveChanges(
      form({ promptMode: 'ref', promptRef: 'builtin:triager', promptText: 'stale copy', description: 'Sorts.', model: 'fast', args: ['--x'] }),
    );
    const value = changes[0].value as Record<string, unknown>;
    expect(value).toEqual({
      harness: 'claude',
      model: 'fast',
      description: 'Sorts.',
      args: ['--x'],
      prompt: 'builtin:triager',
    });
    expect(value).not.toHaveProperty('prompt_text');
  });
});

describe('formFromAgent', () => {
  it('starts an inline agent in text mode', () => {
    const f = formFromAgent(view({ name: 'docs', prompt: '', prompt_text: 'Write docs.' }));
    expect(f.promptMode).toBe('text');
    expect(f.promptText).toBe('Write docs.');
  });

  it('starts a built-in reference in ref mode, with its resolved text kept for switching', () => {
    const f = formFromAgent(
      view({ name: 'improver', origin: 'builtin', prompt: 'builtin:improver', prompt_text: 'Improve things.' }),
    );
    expect(f.promptMode).toBe('ref');
    expect(f.promptRef).toBe('builtin:improver');
    expect(f.promptText).toBe('Improve things.');
  });
});
