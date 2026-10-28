// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { renderMarkdown, sanitizeMarkdownHTML } from './Markdown';

function parse(html: string): HTMLElement {
  const root = document.createElement('div');
  root.innerHTML = html;
  return root;
}

describe('sanitizeMarkdownHTML', () => {
  it('removes forms and inputs, keeping their text', () => {
    const out = sanitizeMarkdownHTML(
      '<p>Log in</p><form action="https://evil.example"><input type="password" name="pw">Sign in</form>',
    );
    const root = parse(out);
    expect(root.querySelector('form')).toBeNull();
    expect(root.querySelector('input')).toBeNull();
    expect(root.textContent).toContain('Sign in');
  });

  it('turns images into links with their alt text and no image element', () => {
    const out = sanitizeMarkdownHTML('<p><img src="https://track.example/p.gif" alt="status"></p>');
    const root = parse(out);
    expect(root.querySelector('img')).toBeNull();
    const link = root.querySelector('a');
    expect(link?.getAttribute('href')).toBe('https://track.example/p.gif');
    expect(link?.textContent).toBe('status');
  });

  it('removes style attributes and <style> elements', () => {
    const out = sanitizeMarkdownHTML(
      '<style>.x{position:fixed}</style><p style="position:fixed;inset:0">overlay</p>',
    );
    const root = parse(out);
    expect(root.querySelector('style')).toBeNull();
    expect(root.querySelector('[style]')).toBeNull();
    expect(out).not.toContain('style');
    expect(root.textContent).toContain('overlay');
  });

  it('drops the href from javascript: links and keeps the link text', () => {
    const out = sanitizeMarkdownHTML('<a href="javascript:alert(1)">click</a>');
    const link = parse(out).querySelector('a');
    expect(link).not.toBeNull();
    expect(link?.hasAttribute('href')).toBe(false);
    expect(link?.textContent).toBe('click');
  });

  it('keeps http(s) and mailto hrefs and forces target and rel', () => {
    const root = parse(
      sanitizeMarkdownHTML(
        '<a href="https://github.com/x/y">gh</a><a href="mailto:a@b.c">mail</a><a href="data:text/html,x">data</a>',
      ),
    );
    const [gh, mail, data] = Array.from(root.querySelectorAll('a'));
    expect(gh?.getAttribute('href')).toBe('https://github.com/x/y');
    expect(mail?.getAttribute('href')).toBe('mailto:a@b.c');
    expect(data?.hasAttribute('href')).toBe(false);
    for (const a of [gh, mail, data]) {
      expect(a?.getAttribute('target')).toBe('_blank');
      expect(a?.getAttribute('rel')).toBe('noopener noreferrer');
    }
  });

  it('removes event handler and data attributes', () => {
    const out = sanitizeMarkdownHTML('<p onclick="alert(1)" data-x="1">hi</p>');
    expect(out).toBe('<p>hi</p>');
  });
});

describe('renderMarkdown', () => {
  it('keeps headings, lists, code, tables and links', () => {
    const md = [
      '# Title',
      '',
      '- one',
      '- two',
      '',
      '```ts',
      'const x = 1;',
      '```',
      '',
      '| a | b |',
      '| - | - |',
      '| 1 | 2 |',
      '',
      '[docs](https://example.com/docs)',
    ].join('\n');
    const root = parse(renderMarkdown(md));
    expect(root.querySelector('h1')?.textContent).toBe('Title');
    expect(root.querySelectorAll('li')).toHaveLength(2);
    const code = root.querySelector('pre code');
    expect(code?.textContent).toContain('const x = 1;');
    expect(code?.classList.contains('language-ts')).toBe(true);
    expect(root.querySelectorAll('table td')).toHaveLength(2);
    expect(root.querySelector('a')?.getAttribute('href')).toBe('https://example.com/docs');
  });

  it('neutralises raw HTML written inside Markdown', () => {
    const root = parse(renderMarkdown('hello\n\n<form><input type="password"></form>\n\n![x](https://t.example/i.png)'));
    expect(root.querySelector('form')).toBeNull();
    expect(root.querySelector('input')).toBeNull();
    expect(root.querySelector('img')).toBeNull();
  });
});
