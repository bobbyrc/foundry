// The inputs for one value in the config. Each reports the new value, or undefined when the
// owner cleared it: the editor then removes the key instead of writing an empty string.
import { useEffect, useState } from 'react';
import type { SchemaType } from '../../types';
import { isEnvRef } from '../../lib/configDoc';

export const SCALAR_TYPES: SchemaType[] = ['string', 'int', 'float', 'bool', 'duration'];

export function isScalarType(type: SchemaType): boolean {
  return SCALAR_TYPES.includes(type);
}

interface ScalarInputProps {
  id: string;
  /** Names the input for screen readers when it has no <label> of its own (bool). */
  labelledBy?: string;
  /** The accessible name, for an input with no visible label of its own (table cells, list rows). */
  label?: string;
  type: SchemaType;
  value: unknown;
  invalid?: boolean;
  onChange: (next: unknown) => void;
}

/** One scalar: text, a number, or a checkbox. ${NAME} references stay as text for any type. */
export function ScalarInput({ id, labelledBy, label, type, value, invalid, onChange }: ScalarInputProps) {
  if (type === 'bool') {
    const checked = value === true;
    return (
      <label className="config-check" htmlFor={id}>
        <input
          id={id}
          type="checkbox"
          aria-labelledby={labelledBy}
          aria-label={labelledBy ? undefined : label}
          checked={checked}
          aria-invalid={invalid || undefined}
          onChange={(event) => {
            // Unchecking a key that was never set leaves it unset.
            if (!event.target.checked && value === undefined) onChange(undefined);
            else onChange(event.target.checked);
          }}
        />
        <span>On</span>
      </label>
    );
  }

  const numeric = type === 'int' || type === 'float';
  const shown = typeof value === 'string' || typeof value === 'number' ? String(value) : '';

  if (numeric && (value === undefined || typeof value === 'number')) {
    return (
      <input
        id={id}
        className="config-input"
        type="number"
        step={type === 'int' ? 1 : 'any'}
        aria-labelledby={labelledBy}
        aria-label={labelledBy ? undefined : label}
        aria-invalid={invalid || undefined}
        value={shown}
        onChange={(event) => {
          const raw = event.target.value;
          if (raw.trim() === '') onChange(undefined);
          else {
            const parsed = Number(raw);
            // A value the browser cannot read as a number is kept as text; the server reports it.
            onChange(Number.isFinite(parsed) ? parsed : raw);
          }
        }}
      />
    );
  }

  return (
    <input
      id={id}
      className="config-input"
      type="text"
      autoComplete="off"
      spellCheck={false}
      aria-labelledby={labelledBy}
      aria-label={labelledBy ? undefined : label}
      aria-invalid={invalid || undefined}
      placeholder={type === 'duration' ? 'e.g. 15m' : undefined}
      value={shown}
      onChange={(event) => {
        const raw = event.target.value;
        if (raw.trim() === '') {
          onChange(undefined);
          return;
        }
        if (numeric && !isEnvRef(raw)) {
          const parsed = Number(raw);
          onChange(Number.isFinite(parsed) ? parsed : raw);
          return;
        }
        onChange(raw);
      }}
    />
  );
}

/** The raw JSON of a value that has no simple form (`any`). It is parsed when the owner leaves it. */
export function AnyInput({
  id,
  value,
  invalid,
  onChange,
}: {
  id: string;
  value: unknown;
  invalid?: boolean;
  onChange: (next: unknown) => void;
}) {
  const json = value === undefined ? '' : JSON.stringify(value, null, 2);
  const [text, setText] = useState(json);
  const [error, setError] = useState<string | null>(null);

  // Show a new value that came from outside (a discard, a reload), but keep what is being typed.
  const valueKey = value === undefined ? 'unset' : JSON.stringify(value);
  useEffect(() => {
    setText(value === undefined ? '' : JSON.stringify(value, null, 2));
    setError(null);
  }, [valueKey]);

  return (
    <>
      <textarea
        id={id}
        className="config-json"
        rows={6}
        spellCheck={false}
        aria-invalid={invalid || undefined}
        value={text}
        onChange={(event) => setText(event.target.value)}
        onBlur={() => {
          if (text.trim() === '') {
            setError(null);
            onChange(undefined);
            return;
          }
          try {
            const parsed: unknown = JSON.parse(text);
            setError(null);
            onChange(parsed);
          } catch (err) {
            setError(`Not valid JSON: ${err instanceof Error ? err.message : String(err)}`);
          }
        }}
      />
      {error ? <p className="error-line">{error}</p> : null}
    </>
  );
}
