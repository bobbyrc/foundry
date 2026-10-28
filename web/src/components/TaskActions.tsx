import { useState } from 'react';
import { useCancelTask, useRetryTask } from '../queries';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';

/** Cancels a task. Needs confirmation; the button reads "Confirm?" for 3 seconds. */
export function CancelTaskButton({ taskId }: { taskId: string }) {
  const cancel = useCancelTask(taskId);
  return (
    <>
      <Button variant="danger" confirm disabled={cancel.isPending} onClick={() => cancel.mutate()}>
        Cancel task
      </Button>
      <ErrorLine error={cancel.error} />
    </>
  );
}

/** Retry opens an optional note for the next attempt, then re-queues the task. */
export function RetryTaskControl({ taskId }: { taskId: string }) {
  const retry = useRetryTask(taskId);
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState('');

  if (!open) {
    return (
      <Button variant="secondary" onClick={() => setOpen(true)}>
        Retry
      </Button>
    );
  }
  return (
    <form
      className="inline-form"
      onSubmit={(event) => {
        event.preventDefault();
        retry.mutate(note.trim(), {
          onSuccess: () => {
            setOpen(false);
            setNote('');
          },
        });
      }}
    >
      <Field label="Note for the next attempt (optional)">
        <textarea rows={2} value={note} onChange={(event) => setNote(event.target.value)} />
      </Field>
      <div className="row-actions">
        <Button type="submit" variant="primary" disabled={retry.isPending}>
          Retry task
        </Button>
        <Button variant="ghost" onClick={() => setOpen(false)}>
          Dismiss
        </Button>
      </div>
      <ErrorLine error={retry.error} />
    </form>
  );
}
