// Court / governance UI (court.md).

export function renderProposalList(container, proposals, { onSelect } = {}) {
  if (!container) return;
  container.replaceChildren();
  (proposals || []).forEach((p) => {
    const article = document.createElement('article');
    article.className = 'subpanel court-proposal-card';
    article.dataset.testid = `proposal-${p.id}`;
    article.innerHTML = `
      <div class="subpanel-header">
        <h3>${esc(p.title)}</h3>
        <span class="badge">${esc(p.status)}</span>
      </div>
      <p>${esc(p.summary || '')}</p>
      <small class="subtle">Votes: ${esc(p.votes || '—')}</small>`;
    article.style.cursor = 'pointer';
    article.addEventListener('click', () => onSelect?.(p));
    container.appendChild(article);
  });
}

export function renderProposalDetail(container, detail) {
  if (!container || !detail) return;
  container.hidden = false;
  const prop = detail.proposal || {};
  const reviews = detail.reviews || [];
  container.innerHTML = `
    <div class="subpanel-header">
      <h3 data-court-detail-title>${esc(prop.title || detail.proposal_id)}</h3>
      <span class="badge">${esc(prop.state || prop.status || 'review')}</span>
    </div>
    <p data-court-detail-summary>${esc(prop.description || prop.summary || '')}</p>
    <div data-court-votes class="court-votes"></div>
    <div class="court-actions" data-testid="court-actions">
      <button type="button" class="primary-button" data-action="approve" data-testid="court-approve">Approve</button>
      <button type="button" class="danger-button" data-action="reject" data-testid="court-reject">Reject</button>
      <button type="button" class="secondary-button" data-action="defer" data-testid="court-defer">Defer</button>
      <button type="button" class="secondary-button" data-action="export" data-testid="court-export">Export Report</button>
    </div>`;
  const votesEl = container.querySelector('[data-court-votes]');
  if (!reviews.length) {
    votesEl.innerHTML = '<p class="subtle">No persona votes yet.</p>';
    return;
  }
  reviews.forEach((v) => {
    const card = document.createElement('article');
    card.className = 'court-vote-card';
    card.dataset.testid = `vote-${v.persona || 'unknown'}`;
    card.innerHTML = `
      <div class="subpanel-header">
        <strong>${esc(v.persona || 'persona')}</strong>
        <span class="badge badge--${esc(v.verdict || 'pending')}">${esc(v.verdict || 'pending')}</span>
      </div>
      <p>${esc(v.comments || v.rationale || '')}</p>`;
    votesEl.appendChild(card);
  });
}

export async function proposalAction(proposalId, action, note = '') {
  const ok = action === 'defer' || confirm(`Confirm ${action} for proposal ${proposalId}?`);
  if (!ok) return null;
  const res = await fetch(`/api/proposals/${proposalId}/${action}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Aegis-Confirmed': '1',
    },
    body: JSON.stringify({ note }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

function esc(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}