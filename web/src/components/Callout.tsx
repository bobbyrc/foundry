import type { ReactNode } from 'react';

/** A boxed note. "danger" is used for blocked reasons and errors; "muted" for plain status notes. */
export function Callout({
  tone = 'danger',
  title,
  children,
}: {
  tone?: 'danger' | 'info' | 'muted';
  title?: string;
  children: ReactNode;
}) {
  return (
    <div className={`callout callout-${tone}`} role={tone === 'danger' ? 'alert' : 'note'}>
      {title ? <strong>{title}. </strong> : null}
      {children}
    </div>
  );
}
