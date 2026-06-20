import { useEffect, useState } from 'react';
import { api, type ProposalAction } from '@/api/client';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { EmptyState } from '@/components/ui/EmptyState';
import { Proposal } from '@/contracts';
import {
  exportProposalConfirmCopy,
  isProposalAction,
  proposalActionConfirmCopy,
  PROPOSAL_ACTION_LABELS,
} from '@/lib/confirmCopy';
import { usePortalStore } from '@/store/portalStore';

type PendingConfirm =
  | { kind: 'action'; proposalId: string; proposalTitle: string; action: ProposalAction }
  | { kind: 'export'; proposalId: string; proposalTitle: string }
  | null;

async function downloadProposalExport(proposalId: string) {
  const res = await api.exportProposal(proposalId);
  if (!res.ok) throw new Error(`Export failed (${res.status})`);
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `proposal-${proposalId}-export.txt`;
  a.click();
  URL.revokeObjectURL(url);
}

export function CourtView() {
  const proposals = usePortalStore((s) => s.proposals);
  const selected = usePortalStore((s) => s.selectedProposal);
  const setSelected = usePortalStore((s) => s.setSelectedProposal);
  const safeMode = usePortalStore((s) => s.dashboard?.safe_mode);
  const [reviews, setReviews] = useState<{ reviews: unknown[] } | null>(null);
  const [pending, setPending] = useState<PendingConfirm>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    if (!selected) {
      setReviews(null);
      return;
    }
    api.proposalReviews(selected.id).then(setReviews).catch(() => setReviews({ reviews: [] }));
  }, [selected]);

  const governanceLocked = safeMode === true;

  const requestAction = (action: string) => {
    if (!selected || governanceLocked) return;
    if (!isProposalAction(action)) {
      setActionError(`Unknown court action: ${action}`);
      return;
    }
    setActionError(null);
    setPending({
      kind: 'action',
      proposalId: selected.id,
      proposalTitle: selected.title,
      action,
    });
  };

  const requestExport = (proposal: Proposal) => {
    setActionError(null);
    setPending({
      kind: 'export',
      proposalId: proposal.id,
      proposalTitle: proposal.title,
    });
  };

  const handleConfirm = async () => {
    if (!pending) return;
    setActionError(null);
    try {
      if (pending.kind === 'action') {
        await api.proposalAction(pending.proposalId, pending.action);
      } else {
        await downloadProposalExport(pending.proposalId);
      }
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Action failed');
    } finally {
      setPending(null);
    }
  };

  const dialogCopy =
    pending?.kind === 'action'
      ? proposalActionConfirmCopy(pending.action, pending.proposalTitle)
      : pending?.kind === 'export'
        ? exportProposalConfirmCopy(pending.proposalTitle)
        : null;

  return (
    <section className="panel content-panel" data-testid="court-panel" data-page="court">
      <header>
        <p className="eyebrow">Governance</p>
        <h1>Court</h1>
        {governanceLocked && (
          <p className="subtle" data-testid="court-safe-mode-notice">
            Safe Mode is ON — court decisions are disabled until Safe Mode is turned off.
          </p>
        )}
      </header>
      <div className="card-grid" data-testid="proposals-list">
        {proposals.length === 0 ? (
          <EmptyState
            testId="court-empty-state"
            eyebrow="Governance"
            title="No proposals yet"
            description="Court decisions appear here when work in a channel triggers a governance review. Start a goal on Home or post in a channel to generate proposals."
            hint="Pending proposals also surface inline in channel activity feeds."
          />
        ) : (
          proposals.map((p: Proposal) => (
            <article
              key={p.id}
              className={`list-card${selected?.id === p.id ? ' active' : ''}`}
              data-testid={`proposal-${p.id}`}
              onClick={() => setSelected(p)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => e.key === 'Enter' && setSelected(p)}
            >
              <strong>{p.title}</strong>
              <span className="badge badge--pending">{p.status}</span>
              <p className="subtle">{p.summary}</p>
              <button
                type="button"
                className="secondary-button"
                data-testid={`quick-export-${p.id}`}
                onClick={(e) => {
                  e.stopPropagation();
                  requestExport(p);
                }}
              >
                Quick Export
              </button>
            </article>
          ))
        )}
      </div>

      {selected && (
        <article className="subpanel" data-testid="court-detail">
          <h2>{selected.title}</h2>
          <p>{selected.summary}</p>
          {reviews?.reviews?.map((r, i) => (
            <div key={i} className="list-card">
              <pre>{JSON.stringify(r, null, 2)}</pre>
            </div>
          ))}
          {actionError && (
            <p className="subtle" role="alert" data-testid="court-action-error">
              {actionError}
            </p>
          )}
          <div className="court-actions" data-testid="court-actions">
            <button
              type="button"
              className="primary-button"
              data-testid="court-approve"
              disabled={governanceLocked}
              onClick={() => requestAction('approve')}
            >
              {PROPOSAL_ACTION_LABELS.approve}
            </button>
            <button
              type="button"
              className="danger-button"
              data-testid="court-reject"
              disabled={governanceLocked}
              onClick={() => requestAction('reject')}
            >
              {PROPOSAL_ACTION_LABELS.reject}
            </button>
            <button
              type="button"
              className="secondary-button"
              data-testid="court-defer"
              disabled={governanceLocked}
              onClick={() => requestAction('defer')}
            >
              {PROPOSAL_ACTION_LABELS.defer}
            </button>
            <button
              type="button"
              className="secondary-button"
              data-testid="court-export"
              onClick={() => requestExport(selected)}
            >
              Export Report
            </button>
          </div>
        </article>
      )}

      <ConfirmDialog
        open={pending !== null}
        title={dialogCopy?.title ?? 'Confirm'}
        message={dialogCopy?.message ?? ''}
        confirmLabel={pending?.kind === 'export' ? 'Export' : 'Continue'}
        variant={pending?.kind === 'action' && pending.action === 'reject' ? 'danger' : 'primary'}
        onConfirm={() => void handleConfirm()}
        onCancel={() => setPending(null)}
        testId="court-confirm-dialog"
      />
    </section>
  );
}
