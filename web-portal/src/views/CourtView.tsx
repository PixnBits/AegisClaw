import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { Proposal } from '@/contracts';
import { usePortalStore } from '@/store/portalStore';

export function CourtView() {
  const proposals = usePortalStore((s) => s.proposals);
  const selected = usePortalStore((s) => s.selectedProposal);
  const setSelected = usePortalStore((s) => s.setSelectedProposal);
  const [reviews, setReviews] = useState<{ reviews: unknown[] } | null>(null);

  useEffect(() => {
    if (!selected) {
      setReviews(null);
      return;
    }
    api.proposalReviews(selected.id).then(setReviews).catch(() => setReviews({ reviews: [] }));
  }, [selected]);

  const handleAction = async (action: string) => {
    if (!selected) return;
    await api.proposalAction(selected.id, action);
  };

  const handleExport = async () => {
    if (!selected) return;
    const res = await api.exportProposal(selected.id);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `proposal-${selected.id}-export.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <section className="panel content-panel" data-testid="court-panel" data-page="court">
      <header>
        <p className="eyebrow">Governance</p>
        <h1>Court</h1>
      </header>
      <div className="card-grid" data-testid="proposals-list">
        {proposals.map((p: Proposal) => (
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
                api.exportProposal(p.id);
              }}
            >
              Quick Export
            </button>
          </article>
        ))}
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
          <div className="court-actions" data-testid="court-actions">
            <button type="button" className="primary-button" data-testid="court-approve" onClick={() => handleAction('approve')}>
              Approve
            </button>
            <button type="button" className="danger-button" data-testid="court-reject" onClick={() => handleAction('reject')}>
              Reject
            </button>
            <button type="button" className="secondary-button" data-testid="court-defer" onClick={() => handleAction('defer')}>
              Defer
            </button>
            <button type="button" className="secondary-button" data-testid="court-export" onClick={handleExport}>
              Export Report
            </button>
          </div>
        </article>
      )}
    </section>
  );
}