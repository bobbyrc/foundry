import { tokenizeJson } from '../lib/highlight';

const CLASS: Record<string, string> = {
  key: 'tok-key',
  string: 'tok-string',
  number: 'tok-number',
  literal: 'tok-literal',
  text: '',
};

export function JsonView({ value }: { value: unknown }) {
  const tokens = tokenizeJson(JSON.stringify(value, null, 2));
  return (
    <pre className="json">
      {tokens.map((token, index) =>
        CLASS[token.kind] ? (
          <span key={index} className={CLASS[token.kind]}>
            {token.text}
          </span>
        ) : (
          token.text
        ),
      )}
    </pre>
  );
}
