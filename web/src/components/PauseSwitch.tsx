import { useCan } from '../auth';
import { useControl, useTogglePause } from '../queries';
import { cx } from '../lib/format';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';

/**
 * Pause or resume the whole factory: the line's cord. While paused no new run starts and no new
 * work is picked up; release verification/rollback and production watching continue.
 */
export function PauseSwitch() {
  const control = useControl();
  const toggle = useTogglePause();
  const paused = control.data?.paused ?? false;
  // Viewers see the state but not the cord: the button is an operator action.
  const canOperate = useCan('operate');

  return (
    <div className={cx('pause-switch', paused && 'is-paused')}>
      <span className="pause-state">
        <span className={cx('lamp', paused ? 'lamp-stop' : 'lamp-run')} aria-hidden="true" />
        {control.data ? (paused ? 'Paused' : 'Running') : '—'}
      </span>
      {canOperate ? (
        <Button variant="secondary" className="pause-button" disabled={!control.data || toggle.isPending} onClick={() => toggle.mutate(!paused)}>
          {paused ? 'Resume' : 'Pause'}
        </Button>
      ) : null}
      {paused ? <span className="pause-note">No new runs start.</span> : null}
      <ErrorLine error={toggle.error ?? control.error} />
    </div>
  );
}
