import { useLiveStatus } from '../events';

export function LiveIndicator() {
  const status = useLiveStatus();
  const live = status === 'live';
  return (
    <div className={`live ${live ? 'live-on' : 'live-off'}`} role="status">
      <span className="dot" aria-hidden="true" />
      {live ? 'live' : 'reconnecting…'}
    </div>
  );
}
