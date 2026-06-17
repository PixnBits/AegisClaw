// Canvas inter-agent pipeline view (canvas.md).

import { renderPipelineStrip, renderNarrowTasks } from './harness.js';

export function renderCanvas(container, state) {
  if (!container || !state) return;
  const goalEl = container.querySelector('[data-canvas-goal]');
  const stripEl = container.querySelector('[data-canvas-pipeline]');
  const gridEl = container.querySelector('[data-canvas-grid]');
  if (goalEl) {
    goalEl.textContent = state.plan?.goal || state.plan?.Goal || 'Parallel agent collaboration';
  }
  const stages = state.plan?.stages || state.plan?.Stages;
  renderPipelineStrip(stripEl, stages);
  if (!gridEl) return;
  gridEl.replaceChildren();
  gridEl.dataset.testid = 'canvas-agent-grid';
  const tasks = state.tasks || [];
  if (!tasks.length) {
    gridEl.innerHTML = '<p class="subtle">No active agents on canvas.</p>';
    return;
  }
  tasks.forEach((task) => {
    const card = document.createElement('article');
    card.className = 'canvas-agent-card';
    card.dataset.testid = `canvas-agent-${task.task_id || task.agent_persona}`;
    card.innerHTML = `
      <strong>${esc(task.agent_persona || 'agent')}</strong>
      <p>${esc(task.scope || '')}</p>
      <span class="badge">${esc(task.current_stage || task.status || '')}</span>
      <button type="button" class="secondary-button" data-trace-link>View Trace</button>`;
    card.querySelector('[data-trace-link]')?.addEventListener('click', () => {
      location.hash = `trace?agent=${encodeURIComponent(task.agent_persona || task.task_id)}`;
    });
    gridEl.appendChild(card);
  });
}

function esc(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}