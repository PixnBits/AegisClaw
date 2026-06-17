// Single-agent trace view (single-agent-trace.md).

const PHASE_ORDER = ['Observe', 'Think', 'Plan', 'Act', 'Judge'];

export function renderTrace(container, trace) {
  if (!container) return;
  container.replaceChildren();
  if (!trace?.phases?.length) {
    container.innerHTML = '<p class="subtle" data-testid="trace-empty">No trace phases yet.</p>';
    return;
  }
  const grouped = groupPhases(trace.phases);
  PHASE_ORDER.forEach((phaseName) => {
    const entries = grouped[phaseName];
    if (!entries?.length) return;
    const section = document.createElement('section');
    section.className = 'trace-phase';
    section.dataset.testid = `trace-phase-${phaseName.toLowerCase()}`;
    section.innerHTML = `<h4 class="trace-phase__title">${phaseName}</h4>`;
    entries.forEach((entry, i) => {
      const row = document.createElement('details');
      row.className = 'trace-entry';
      row.dataset.testid = `trace-entry-${phaseName}-${i}`;
      row.open = phaseName === 'Act' && i === entries.length - 1;
      row.innerHTML = `
        <summary>${esc(entry.summary || entry.tool || phaseName)} <small>${esc(entry.ts || '')}</small></summary>
        <div class="trace-entry__body">
          ${entry.tool ? `<p><strong>Tool:</strong> ${esc(entry.tool)}</p>` : ''}
          <p>${esc(entry.summary || '')}</p>
          <span class="badge">${esc(entry.status || 'ok')}</span>
        </div>`;
      section.appendChild(row);
    });
    container.appendChild(section);
  });
}

function groupPhases(phases) {
  const out = {};
  phases.forEach((p) => {
    const name = p.phase || 'Observe';
    out[name] = out[name] || [];
    out[name].push(p);
  });
  return out;
}

function esc(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}