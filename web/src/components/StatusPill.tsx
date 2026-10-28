import { isPulsing, toneFor, type PillKind } from '../lib/status';
import { humanize, cx } from '../lib/format';

export function StatusPill({ status, kind = 'task' }: { status: string; kind?: PillKind }) {
  const tone = toneFor(kind, status);
  return (
    <span className={cx('pill', `tone-${tone}`)}>
      <span className={cx('dot', isPulsing(kind, status) && 'pulse')} aria-hidden="true" />
      {humanize(status)}
    </span>
  );
}
