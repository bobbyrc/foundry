import { useState } from 'react';
import { Link } from 'react-router-dom';
import type { Approval } from '../types';
import { useCan } from '../auth';
import { useDecideApproval, useTask } from '../queries';
import { humanize, safeHref, shortSha } from '../lib/format';
import { latestDoneReview } from '../lib/review';
import { ClampedMarkdown } from './ClampedMarkdown';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { Field } from './Field';
import { RelTime } from './RelTime';
import { RiskBadge } from './RiskBadge';
import { StatusPill } from './StatusPill';
import { IdChip } from './IdChip';

/**
 * An approval shows the gate it is asking you to open: the risk and why, and the one commit the
 * approval covers. The decision comes before the longer agent-written context, so it is in
 * reach on a phone. A pending approval is lit like a call lamp.
 */
export function ApprovalCard({ approval, showTask = true }: { approval: Approval; showTask?: boolean }) {
  const [note, setNote] = useState('');
  const decide = useDecideApproval(approval.id);
  const canOperate = useCan('operate');
  const pending = approval.status === 'pending';
  const detail = useTask(approval.task_id);
  const task = detail.data?.task;
  const summary = task?.summary;
  const review = latestDoneReview(detail.data?.children);
  const prHref = safeHref(task?.pr_url);

  return (
    <article className="call-item approval-card">
      <div className="call-title">
        <span className={`lamp ${pending ? 'lamp-call' : 'lamp-off'}`} aria-hidden="true" />
        <h3>{approval.title}</h3>
        {pending ? null : <StatusPill kind="approval" status={approval.status} />}
      </div>
      <p className="call-meta">
        {pending ? (canOperate ? 'Waiting on you' : 'Waiting on an operator') : humanize(approval.status)} ·{' '}
        {humanize(approval.kind)} · {approval.project}
        {showTask ? (
          <>
            {' · task '}
            <Link to={`/tasks/${approval.task_id}`}>
              <IdChip id={approval.task_id} />
            </Link>
          </>
        ) : null}
        {' · requested '}
        <RelTime value={approval.created_at} />
      </p>
      <div className="call-body">
        {task ? (
          <dl className="gate">
            <dt>Risk</dt>
            <dd>
              <RiskBadge risk={task.risk} />
              {task.risk_reasons && task.risk_reasons.length > 0 ? (
                <ul className="reasons">
                  {task.risk_reasons.map((reason) => (
                    <li key={reason}>{reason}</li>
                  ))}
                </ul>
              ) : null}
            </dd>
            {approval.head_sha ? (
              <>
                <dt>Covers commit</dt>
                <dd>
                  <code title={approval.head_sha}>{shortSha(approval.head_sha)}</code>
                  {task.pr_number ? (
                    <>
                      {' on '}
                      {prHref ? (
                        <a href={prHref} target="_blank" rel="noreferrer">
                          PR #{task.pr_number}
                        </a>
                      ) : (
                        `PR #${task.pr_number}`
                      )}
                    </>
                  ) : null}
                  <span className="muted"> · a new push withdraws this approval</span>
                </dd>
              </>
            ) : null}
          </dl>
        ) : null}
        <ClampedMarkdown text={approval.reason} lines={3} />
        {approval.note ? <p className="small">Note: {approval.note}</p> : null}
        {pending && canOperate ? (
          <div className="decide">
            <Field label="Note (optional)">
              <input type="text" value={note} onChange={(event) => setNote(event.target.value)} />
            </Field>
            <div className="row-actions">
              <Button variant="primary" disabled={decide.isPending} onClick={() => decide.mutate({ approve: true, note: note.trim() })}>
                {approval.kind === 'merge' ? 'Approve merge' : 'Approve'}
              </Button>
              <Button variant="danger" confirm disabled={decide.isPending} onClick={() => decide.mutate({ approve: false, note: note.trim() })}>
                Reject
              </Button>
            </div>
            <ErrorLine error={decide.error} />
          </div>
        ) : null}
        {summary ? (
          <section className="approval-section">
            <h4>What changed</h4>
            <ClampedMarkdown text={summary} lines={3} />
          </section>
        ) : null}
        {detail.isSuccess ? (
          <section className="approval-section">
            <h4>Reviewer</h4>
            {review?.summary ? <ClampedMarkdown text={review.summary} lines={3} /> : <p className="muted small">No review yet</p>}
          </section>
        ) : null}
      </div>
    </article>
  );
}
