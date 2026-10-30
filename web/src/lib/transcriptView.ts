// The owner's choice between the conversation and terminal layouts of a transcript.
export type TranscriptView = 'conversation' | 'terminal';

const KEY = 'onett.transcriptView';

/** Storage can be missing or throw (private windows, blocked site data); the default always works. */
export function readTranscriptView(): TranscriptView {
  try {
    return localStorage.getItem(KEY) === 'terminal' ? 'terminal' : 'conversation';
  } catch {
    return 'conversation';
  }
}

export function writeTranscriptView(v: TranscriptView): void {
  try {
    localStorage.setItem(KEY, v);
  } catch {
    // The choice just won't be remembered.
  }
}
