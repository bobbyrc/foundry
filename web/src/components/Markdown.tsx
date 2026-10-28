import { useMemo } from 'react';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import type { Config } from 'dompurify';
import { safeHref } from '../lib/format';

// Agent and GitHub text is attacker-controlled. Everything rendered here goes through an explicit
// allowlist: no forms, inputs, images, styles or scripts, and links limited to http(s) and mailto.

const ALLOWED_TAGS = [
  'p', 'br', 'hr', 'a', 'ul', 'ol', 'li', 'code', 'pre', 'em', 'strong', 'del',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'blockquote', 'table', 'thead', 'tbody', 'tr', 'th', 'td',
];

const PURIFY_CONFIG: Config = {
  ALLOWED_TAGS,
  ALLOWED_ATTR: ['href', 'title', 'class'],
  FORBID_ATTR: ['style'],
  ALLOW_DATA_ATTR: false,
};

// marked tags fenced code with a language hint, e.g. class="language-ts". Only that shape is kept.
const LANGUAGE_CLASS = /^language-[\w#+.-]+$/;

const MAILTO = /^mailto:/i;

/** Only http(s) and mailto links survive. Anything else (javascript:, data:, relative) is dropped. */
function safeMarkdownHref(href: string): string | undefined {
  return safeHref(href) ?? (MAILTO.test(href) ? href : undefined);
}

let hooked = false;

function ensureHooks(): void {
  if (hooked) return;
  hooked = true;

  DOMPurify.addHook('uponSanitizeAttribute', (node, data) => {
    if (data.attrName === 'class') {
      // Only code blocks carry a language class; it is never a styling hook.
      const isCode = node.nodeName === 'CODE';
      data.keepAttr = isCode && LANGUAGE_CLASS.test(data.attrValue);
    }
  });

  DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    if (!(node instanceof Element) || node.nodeName !== 'A') return;
    const href = node.getAttribute('href');
    if (href === null || safeMarkdownHref(href) === undefined) {
      node.removeAttribute('href');
    }
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noopener noreferrer');
  });
}

/**
 * Sanitises untrusted HTML for the Markdown view. Images are replaced by links to their source
 * (or their alt text when the source is not a safe link). The DOM is parsed in an inert document,
 * so nothing loads or runs during that step.
 */
export function sanitizeMarkdownHTML(html: string): string {
  ensureHooks();

  const doc = new DOMParser().parseFromString(html, 'text/html');
  for (const img of Array.from(doc.querySelectorAll('img'))) {
    const link = doc.createElement('a');
    const src = img.getAttribute('src') ?? '';
    const href = safeMarkdownHref(src);
    if (href !== undefined) link.setAttribute('href', href);
    link.textContent = img.getAttribute('alt') || src || 'image';
    img.replaceWith(link);
  }

  return DOMPurify.sanitize(doc.body.innerHTML, PURIFY_CONFIG);
}

/** Markdown to sanitised HTML. Agent and user text is untrusted, so the sanitiser always runs. */
export function renderMarkdown(source: string): string {
  const html = marked.parse(source, { async: false }) as string;
  return sanitizeMarkdownHTML(html);
}

export function Markdown({ text }: { text: string }) {
  const html = useMemo(() => renderMarkdown(text), [text]);
  return <div className="markdown" dangerouslySetInnerHTML={{ __html: html }} />;
}
