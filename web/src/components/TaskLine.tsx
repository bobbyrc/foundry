// The task's line drawn as lamps: one square per station, joined by a rule. The rules live in
// lib/taskLine.ts; this only draws them, and every lamp also has a text label for screen readers.
import { taskLine, type Station } from '../lib/taskLine';
import { cx } from '../lib/format';
import type { TaskStatus } from '../types';

const STOP_WORD: Partial<Record<TaskStatus, string>> = { blocked: 'Blocked', failed: 'Failed', cancelled: 'Cancelled' };

const STATE_WORD: Record<Station['state'], string> = {
  passed: 'passed',
  current: 'current',
  stopped: 'stopped',
  skipped: 'skipped',
  ahead: 'not reached',
};

/** The lamp's colour class, from its state and tone. */
function lampClass(station: Station, status: TaskStatus): string {
  switch (station.state) {
    case 'passed':
      return 'task-lamp--done';
    case 'skipped':
      return 'task-lamp--skipped';
    case 'ahead':
      return 'task-lamp--ahead';
    case 'current':
      return `task-lamp--${station.tone === 'run' ? 'run' : station.tone === 'call' ? 'call' : 'off'}`;
    case 'stopped':
      return status === 'cancelled' ? 'task-lamp--off-light' : 'task-lamp--stop';
  }
}

export function TaskLine({ status, hasPR, approvalCount }: { status: TaskStatus; hasPR: boolean; approvalCount: number }) {
  const stations = taskLine({ status, hasPR, approvalCount });
  return (
    <div className="task-line-scroll">
      <ol className="task-line" aria-label="Progress">
        {stations.map((station) => {
          const stopped = station.state === 'stopped';
          const label = stopped ? (STOP_WORD[status] ?? station.label) : station.label;
          const current = station.state === 'current' || stopped;
          const pulse = station.state === 'current' && status === 'running';
          return (
            <li
              key={station.key}
              className={cx('task-station', `task-station--${station.state}`)}
              aria-current={current ? 'step' : undefined}
            >
              <span className={cx('task-lamp', lampClass(station, status), pulse && 'pulse')} aria-hidden="true" />
              <span className="task-station-label" aria-hidden="true">
                {label}
              </span>
              <span className="sr-only">{`${station.label}: ${stopped ? label.toLowerCase() : STATE_WORD[station.state]}`}</span>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
