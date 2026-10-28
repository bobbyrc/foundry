import { useMemo } from 'react';
import { marked } from 'marked';
import DOMPurify from 'dompurify';

let hooked = false;

function ensureHook(): void {
  if (hooked) return;
  hooked = true;
  DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    if (node instanceof HTMLAnchorElement) {
      node.target = '_blank';
      node.rel = 'noopener noreferrer';
    }
  });
}

/** Markdown to sanitised HTML. Agent and user text is untrusted, so DOMPurify always runs. */
export function renderMarkdown(source: string): string {
  ensureHook();
  const html = marked.parse(source, { async: false }) as string;
  return DOMPurify.sanitize(html);
}

export function Markdown({ text }: { text: string }) {
  const html = useMemo(() => renderMarkdown(text), [text]);
  return <div className="markdown" dangerouslySetInnerHTML={{ __html: html }} />;
}
