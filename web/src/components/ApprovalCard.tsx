import { useState } from 'react';
import { Link } from 'react-router-dom';
import type { Approval } from '../types';
import { useDecideApproval, useTask } from '../queries';
import { latestDoneReview } from '../lib/review';
import { ClampedMarkdown } from './ClampedMarkdown';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';
import { Markdown } from './Markdown';
import { RelTime } from './RelTime';
import { StatusPill } from './StatusPill';
import { IdChip } from './IdChip';

export function ApprovalCard({ approval, showTask = true }: { approval: Approval; showTask?: boolean }) {
  const [note, setNote] = useState('');
  const decide = useDecideApproval(approval.id);
  const pending = approval.status === 'pending';
  const detail = useTask(approval.task_id);
  const summary = detail.data?.task.summary;
  const review = latestDoneReview(detail.data?.children);

  return (
    <article className="card approval-card">
      <header className="card-head">
        <h3>{approval.title}</h3>
        <span className="tag">{approval.kind}</span>
        <StatusPill kind="approval" status={approval.status} />
      </header>
      <p className="muted small">
        {approval.project}
        {showTask ? (
          <>
            {' · task '}
            <Link to={`/tasks/${approval.task_id}`}>
              <IdChip id={approval.task_id} />
            </Link>
          </>
        ) : null}
        {' · created '}
        <RelTime value={approval.created_at} />
      </p>
      <Markdown text={approval.reason} />
      {summary ? (
        <section className="approval-section">
          <h4>What changed</h4>
          <ClampedMarkdown text={summary} lines={6} />
        </section>
      ) : null}
      {detail.isSuccess ? (
        <section className="approval-section">
          <h4>Reviewer</h4>
          {review?.summary ? <ClampedMarkdown text={review.summary} lines={4} /> : <p className="muted small">No review yet</p>}
        </section>
      ) : null}
      {approval.note ? <p className="small">Note: {approval.note}</p> : null}
      {pending ? (
        <div className="decide">
          <Field label="Note (optional)">
            <input type="text" value={note} onChange={(event) => setNote(event.target.value)} />
          </Field>
          <div className="row-actions">
            <Button variant="primary" disabled={decide.isPending} onClick={() => decide.mutate({ approve: true, note: note.trim() })}>
              Approve
            </Button>
            <Button variant="danger" confirm disabled={decide.isPending} onClick={() => decide.mutate({ approve: false, note: note.trim() })}>
              Reject
            </Button>
          </div>
          <ErrorLine error={decide.error} />
        </div>
      ) : null}
    </article>
  );
}
