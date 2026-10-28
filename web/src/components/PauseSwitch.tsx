import { useControl, useTogglePause } from '../queries';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';

/**
 * Pause or resume the whole factory. While paused no new run starts and no new work is picked up;
 * release verification/rollback and production watching continue.
 */
export function PauseSwitch() {
  const control = useControl();
  const toggle = useTogglePause();
  const paused = control.data?.paused ?? false;

  return (
    <div className="pause-switch">
      <Button
        variant={paused ? 'danger-fill' : 'secondary'}
        className="pause-button"
        disabled={!control.data || toggle.isPending}
        onClick={() => toggle.mutate(!paused)}
      >
        {paused ? 'Paused — no new runs. Resume' : 'Pause factory'}
      </Button>
      <ErrorLine error={toggle.error ?? control.error} />
    </div>
  );
}
