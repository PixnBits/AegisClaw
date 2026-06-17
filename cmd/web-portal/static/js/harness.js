// Harness / pipeline visualization (harness-pipeline-data-model.md).

import { PIPELINE_STAGES } from './contracts.js';

export function renderPipelineStrip(container, stages) {
  if (!container) return;
  container.replaceChildren();
  const list = stages?.length ? stages : defaultStages();
  list.forEach((stage) => {
    const el = document.createElement('div');
    el.className = `pipeline-stage pipeline-stage--${stage.status || 'pending'}`;
    el.dataset.testid = `pipeline-stage-${slug(stage.name)}`;
    el.innerHTML = `<span class="pipeline-stage__name">${stage.name}</span>`;
    container.appendChild(el);
  });
}

export function renderNarrowTasks(container, tasks) {
  if (!container) return;
  container.replaceChildren();
  if (!tasks?.length) {
    const empty = document.createElement('p');
    empty.className = 'subtle';
    empty.textContent = 'No narrow tasks yet — submit a goal to start decomposition.';
    container.appendChild(empty);
    return;
  }
  tasks.forEach((task) => {
    const card = document.createElement('article');
    card.className = 'harness-task-card';
    card.dataset.testid = `task-${task.task_id}`;
    card.innerHTML = `
      <div class="harness-task-card__header">
        <strong>${escapeHtml(task.agent_persona || 'agent')}</strong>
        <span class="badge badge--${task.status || 'pending'}">${task.current_stage || '—'}</span>
      </div>
      <p class="harness-task-card__scope">${escapeHtml(task.scope || '')}</p>
      <div class="harness-task-card__progress" aria-label="Progress ${task.progress || 0}%">
        <div class="harness-task-card__bar" style="width:${Math.min(100, task.progress || 0)}%"></div>
      </div>`;
    container.appendChild(card);
  });
}

export function renderHarnessOverview(container, state) {
  if (!container || !state) return;
  const goalEl = container.querySelector('[data-harness-goal]');
  const stripEl = container.querySelector('[data-harness-pipeline]');
  const tasksEl = container.querySelector('[data-harness-tasks]');
  if (goalEl) {
    goalEl.textContent = state.plan?.goal || 'No active plan — use the command bar to start.';
  }
  renderPipelineStrip(stripEl, state.plan?.stages);
  renderNarrowTasks(tasksEl, state.tasks);
}

export function applyHarnessEvent(state, event) {
  if (!event?.type) return state;
  const next = structuredClone(state || { plan: null, tasks: [] });
  switch (event.type) {
    case 'harness.plan.created':
      next.plan = {
        plan_id: event.plan_id,
        channel_id: event.channel_id,
        goal: event.goal,
        stages: event.stages || defaultStages(),
        status: 'active',
      };
      break;
    case 'harness.task.assigned':
      next.tasks = next.tasks || [];
      next.tasks.push({
        task_id: event.task_id,
        plan_id: event.plan_id,
        agent_persona: event.agent_persona,
        scope: event.scope,
        current_stage: event.current_stage,
        status: 'active',
        progress: 0,
      });
      break;
    case 'harness.task.progress':
      next.tasks = (next.tasks || []).map((t) =>
        t.task_id === event.task_id
          ? { ...t, progress: event.progress, current_stage: event.current_stage, summary: event.summary }
          : t,
      );
      break;
    case 'harness.stage.transition':
      if (next.plan?.stages) {
        next.plan.stages = next.plan.stages.map((s) =>
          s.name === event.stage ? { ...s, status: event.status } : s,
        );
      }
      break;
    default:
      break;
  }
  return next;
}

function defaultStages() {
  return PIPELINE_STAGES.map((name, i) => ({
    name,
    status: i === 0 ? 'in_progress' : 'pending',
  }));
}

function slug(name) {
  return String(name || '').toLowerCase().replace(/\s+/g, '-');
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}