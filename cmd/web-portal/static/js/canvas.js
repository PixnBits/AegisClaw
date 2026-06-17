// Canvas inter-agent pipeline view (canvas.md).

import { PIPELINE_STAGES } from './contracts.js';
import { renderPipelineStrip } from './harness.js';

const STAGE_ORDER = PIPELINE_STAGES;

export function renderCanvas(container, state, { onChannelLink } = {}) {
  if (!container || !state) return;
  const goalEl = document.querySelector('[data-canvas-goal]');
  const stripEl = container.querySelector('[data-canvas-pipeline]');
  const gridEl = container.querySelector('[data-canvas-grid]');
  const channelLink = document.getElementById('canvasChannelLink');
  const artifactsList = document.getElementById('canvasArtifactsList');

  const goal = state.plan?.goal || state.plan?.Goal || 'Parallel agent collaboration';
  if (goalEl) goalEl.textContent = goal;

  const channelId = state.channel_id || state.plan?.channel_id || 'main';
  if (channelLink) {
    channelLink.textContent = channelId;
    channelLink.onclick = () => {
      if (typeof onChannelLink === 'function') onChannelLink(channelId);
      else {
        location.hash = 'channels';
      }
    };
  }

  const stages = state.plan?.stages || state.plan?.Stages;
  renderPipelineStrip(stripEl, stages);

  if (!gridEl) return;
  gridEl.replaceChildren();
  gridEl.dataset.testid = 'canvas-agent-grid';
  gridEl.className = 'canvas-stage-board';

  const tasks = state.tasks || [];
  if (!tasks.length) {
    const empty = document.createElement('p');
    empty.className = 'subtle';
    empty.textContent = 'No active agents on canvas.';
    gridEl.appendChild(empty);
    renderArtifacts(artifactsList, state, goal);
    return;
  }

  const byStage = groupTasksByStage(tasks);
  STAGE_ORDER.forEach((stageName) => {
    const columnTasks = byStage[stageName] || [];
    if (!columnTasks.length) return;
    const col = document.createElement('section');
    col.className = 'canvas-stage-column';
    col.dataset.testid = `canvas-stage-${slug(stageName)}`;
    col.innerHTML = `<h4 class="canvas-stage-column__title">${stageName}</h4>`;
    columnTasks.forEach((task) => {
      const card = document.createElement('article');
      card.className = 'canvas-agent-card';
      card.dataset.testid = `canvas-agent-${task.task_id || task.agent_persona}`;
      card.innerHTML = `
        <strong>${esc(task.agent_persona || 'agent')}</strong>
        <p>${esc(task.scope || '')}</p>
        <span class="badge">${esc(task.current_stage || task.status || '')}</span>
        <button type="button" class="secondary-button" data-trace-link>Trace</button>`;
      card.querySelector('[data-trace-link]')?.addEventListener('click', () => {
        location.hash = `trace?agent=${encodeURIComponent(task.agent_persona || task.task_id)}`;
      });
      col.appendChild(card);
    });
    gridEl.appendChild(col);
  });

  renderArtifacts(artifactsList, state, goal);
}

function groupTasksByStage(tasks) {
  const out = {};
  tasks.forEach((task) => {
    const stage = task.current_stage || task.status || 'Execute';
    out[stage] = out[stage] || [];
    out[stage].push(task);
  });
  return out;
}

function renderArtifacts(listEl, state, goal) {
  if (!listEl) return;
  listEl.replaceChildren();
  const items = [];
  if (goal) items.push({ title: 'Plan goal', body: goal });
  (state.tasks || []).slice(0, 4).forEach((t) => {
    items.push({ title: `${t.agent_persona || 'agent'} scope`, body: t.scope || '—' });
  });
  if (!items.length) {
    const li = document.createElement('li');
    li.className = 'list-card';
    li.innerHTML = '<span>No artifacts yet</span><small>Agent outputs will appear here.</small>';
    listEl.appendChild(li);
    return;
  }
  items.forEach((item) => {
    const li = document.createElement('li');
    li.className = 'list-card';
    li.innerHTML = `<span>${esc(item.title)}</span><small>${esc(item.body)}</small>`;
    listEl.appendChild(li);
  });
}

function slug(name) {
  return String(name || '').toLowerCase().replace(/\s+/g, '-');
}

function esc(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}