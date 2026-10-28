import { useState } from 'react';
import { useApprovals } from '../queries';
import { ApprovalCard } from '../components/ApprovalCard';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { cx } from '../lib/format';

export function Approvals() {
  const [scope, setScope] = useState<'pending' | 'all'>('pending');
  const approvals = useApprovals(scope);

  return (
    <>
      <PageHeader
        title="Approvals"
        actions={
          <div className="segmented" role="group" aria-label="Approval scope">
            <button type="button" className={cx(scope === 'pending' && 'on')} aria-pressed={scope === 'pending'} onClick={() => setScope('pending')}>
              Pending
            </button>
            <button type="button" className={cx(scope === 'all' && 'on')} aria-pressed={scope === 'all'} onClick={() => setScope('all')}>
              All
            </button>
          </div>
        }
      />
      <ErrorLine error={approvals.error} />
      {approvals.isSuccess && approvals.data.length === 0 ? (
        <Empty>
          {scope === 'pending'
            ? 'No pending approvals. Merges and unblocks that need you will appear here.'
            : 'No approvals yet.'}
        </Empty>
      ) : null}
      {approvals.data?.map((approval) => (
        <ApprovalCard key={approval.id} approval={approval} />
      ))}
    </>
  );
}
