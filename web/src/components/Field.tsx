import type { ReactNode } from 'react';
import { cx } from '../lib/format';

/** A labelled control. Wrapping the input in the label gives it an accessible name. */
export function Field({ label, children, hint, className }: { label: string; children: ReactNode; hint?: ReactNode; className?: string }) {
  return (
    <label className={cx('field', className)}>
      <span className="field-label">{label}</span>
      {children}
      {hint ? <span className="field-hint">{hint}</span> : null}
    </label>
  );
}
