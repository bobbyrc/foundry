import { useState } from 'react';
import { shortId } from '../lib/format';

/** The first 8 characters of an ID; clicking copies the full ID. */
export function IdChip({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard may be unavailable (insecure context); the title still shows the full ID.
    }
  };
  return (
    <button type="button" className="id-chip" title={copied ? 'Copied' : `${id} (click to copy)`} onClick={() => void copy()}>
      {copied ? 'copied' : shortId(id)}
    </button>
  );
}
