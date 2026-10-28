// Tokenises pretty-printed JSON for the config view. Output is rendered as React text nodes,
// never as HTML, so config values cannot inject markup.
export type TokenKind = 'key' | 'string' | 'number' | 'literal' | 'text';

export interface Token {
  kind: TokenKind;
  text: string;
}

const PATTERN = /("(?:\\.|[^"\\])*")(\s*:)?|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false|null)\b/g;

export function tokenizeJson(src: string): Token[] {
  const tokens: Token[] = [];
  let last = 0;
  for (const match of src.matchAll(PATTERN)) {
    const start = match.index;
    if (start > last) tokens.push({ kind: 'text', text: src.slice(last, start) });
    if (match[1] !== undefined) {
      if (match[2]) {
        tokens.push({ kind: 'key', text: match[1] });
        tokens.push({ kind: 'text', text: match[2] });
      } else {
        tokens.push({ kind: 'string', text: match[1] });
      }
    } else if (match[3] !== undefined) {
      tokens.push({ kind: 'number', text: match[3] });
    } else {
      tokens.push({ kind: 'literal', text: match[4] ?? match[0] });
    }
    last = start + match[0].length;
  }
  if (last < src.length) tokens.push({ kind: 'text', text: src.slice(last) });
  return tokens;
}
