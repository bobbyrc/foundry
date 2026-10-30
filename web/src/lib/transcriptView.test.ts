import { afterEach, describe, expect, it, vi } from 'vitest';
import { readTranscriptView, writeTranscriptView } from './transcriptView';

function memoryStorage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
  };
}

afterEach(() => vi.unstubAllGlobals());

describe('transcript view preference', () => {
  it('defaults to conversation', () => {
    vi.stubGlobal('localStorage', memoryStorage());
    expect(readTranscriptView()).toBe('conversation');
  });
  it('reads back a written value', () => {
    vi.stubGlobal('localStorage', memoryStorage());
    writeTranscriptView('terminal');
    expect(readTranscriptView()).toBe('terminal');
  });
  it('ignores an unknown stored value', () => {
    const s = memoryStorage();
    s.setItem('onett.transcriptView', 'bogus');
    vi.stubGlobal('localStorage', s);
    expect(readTranscriptView()).toBe('conversation');
  });
  it('falls back to conversation when storage throws', () => {
    const boom = () => {
      throw new Error('denied');
    };
    vi.stubGlobal('localStorage', { getItem: boom, setItem: boom });
    expect(readTranscriptView()).toBe('conversation');
    expect(() => writeTranscriptView('terminal')).not.toThrow();
  });
});
