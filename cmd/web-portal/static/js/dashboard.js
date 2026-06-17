// Dashboard active work (dashboard.md).

export function renderActiveWork(container, items, { onPause, onTrace, onCanvas, onCourt } = {}) {
  if (!container) return;
  container.replaceChildren();
  if (!items?.length) {
    const empty = document.createElement('p');
    empty.className = 'subtle';
    empty.dataset.testid = 'active-work-empty';
    empty.textContent = 'No active work — start a goal from Home.';
    container.appendChild(empty);
    return;
  }
  items.forEach((item) => {
    const card = document.createElement('article');
    card.className = 'active-work-card';
    card.dataset.testid = `active-work-${item.id}`;
    card.innerHTML = `
      <div class="active-work-card__header">
        <strong>${esc(item.persona || 'agent')}</strong>
        <span class="badge">${esc(item.stage || '—')}</span>
      </div>
      <p class="active-work-card__scope">${esc(item.scope || '')}</p>
      <small class="subtle">${esc(item.channel_id ? `channel: ${item.channel_id}` : '')} · ${esc(item.progress || item.status || '')}</small>
      <div class="active-work-card__actions"></div>`;
    const actions = card.querySelector('.active-work-card__actions');
    if (item.proposal_id && onCourt) {
      actions.append(btn('Review', () => onCourt(item), 'court-action'));
    } else {
      if (onTrace) actions.append(btn('Trace', () => onTrace(item), 'trace-action'));
      if (onCanvas) actions.append(btn('Canvas', () => onCanvas(item), 'canvas-action'));
      if (onPause) actions.append(btn('Pause', () => onPause(item), 'pause-action'));
    }
    container.appendChild(card);
  });
}

export function filterActiveWork(items, filter) {
  if (!filter || filter === 'all') return items || [];
  return (items || []).filter((item) => {
    if (filter === 'proposals') return !!item.proposal_id;
    if (filter === 'background') return !item.proposal_id && item.stage === 'Execute';
    if (filter === 'channel') return !!item.channel_id;
    return true;
  });
}

function btn(label, fn, testid) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'secondary-button';
  b.textContent = label;
  if (testid) b.dataset.testid = testid;
  b.addEventListener('click', fn);
  return b;
}

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}