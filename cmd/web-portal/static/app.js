import { EVENT } from './js/contracts.js';
import { RealtimeClient } from './js/realtime.js';
import { renderHarnessOverview, applyHarnessEvent } from './js/harness.js';
import { renderActiveWork, filterActiveWork } from './js/dashboard.js';
import { renderProposalList, renderProposalDetail, proposalAction } from './js/court.js';
import { renderCanvas } from './js/canvas.js';
import { renderTrace } from './js/trace.js';

const state = {
  harnessByChannel: {},
  currentChannel: null,
  planPreview: null,
  activeWork: [],
  proposals: [],
  selectedProposal: null,
  traceAgentId: null,
  dashboardFilter: 'all',
  monitoring: null,
};

const PAGE_TITLES = {
  home: 'Home',
  channels: 'Channels',
  dashboard: 'Dashboard',
  court: 'Court / Governance',
  agents: 'Agents',
  skills: 'Skills Registry',
  audit: 'Audit Log',
  settings: 'Settings',
  monitoring: 'Dashboard',
  teams: 'Team Workspace',
  canvas: 'Canvas',
  trace: 'Single-Agent Trace',
};

const LAYOUT_BY_PAGE = {
  home: 'layout--home',
  channels: 'layout--channels',
  dashboard: 'layout--dashboard',
  canvas: 'layout--canvas',
  trace: 'layout--trace',
};

const elements = {
  workspaceGrid: document.getElementById('workspaceGrid'),
  systemStatusLabel: document.getElementById('systemStatusLabel'),
  runtimeLabel: document.getElementById('runtimeLabel'),
  notificationCount: document.getElementById('notificationCount'),
  safeModeLabel: document.getElementById('safeModeLabel'),
  connectionLabel: document.getElementById('connectionStatusLabel'),
  sidebarChannelsList: document.getElementById('sidebarChannelsList'),
  recentActivityList: document.getElementById('recentActivityList'),
  homeRecentList: document.getElementById('homeRecentList'),
  skillsList: document.getElementById('skillsList'),
  proposalsList: document.getElementById('proposalsList'),
  channelEmptyState: document.getElementById('channelEmptyState'),
  newChannelForm: document.getElementById('newChannelForm'),
  newChannelId: document.getElementById('newChannelId'),
  channelDetail: document.getElementById('channelDetail'),
  selectedChannelId: document.getElementById('selectedChannelId'),
  membersList: document.getElementById('membersList'),
  addMemberForm: document.getElementById('addMemberForm'),
  newMemberRole: document.getElementById('newMemberRole'),
  archiveChannelBtn: document.getElementById('archiveChannelBtn'),
  harnessOverview: document.getElementById('harnessOverview'),
  planPreview: document.getElementById('planPreview'),
  livePulseAgents: document.getElementById('livePulseAgents'),
  livePulseProposals: document.getElementById('livePulseProposals'),
  activeWorkList: document.getElementById('activeWorkList'),
  courtDetail: document.getElementById('courtDetail'),
  canvasRoot: document.querySelector('[data-canvas-root]'),
  traceTimeline: document.getElementById('traceTimeline'),
  agentsList: document.getElementById('agentsList'),
  harnessTeaserGoal: document.getElementById('harnessTeaserGoal'),
  channelContextSummary: document.getElementById('channelContextSummary'),
};

const realtime = new RealtimeClient({
  onMessage: handleRealtimeMessage,
  onStatus: updateConnectionStatus,
});

function updateConnectionStatus(mode) {
  if (!elements.connectionLabel) return;
  const labels = { stomp: 'Conn STOMP', 'sse-fallback': 'Conn SSE', disconnected: 'Conn Off' };
  elements.connectionLabel.textContent = labels[mode] || 'Conn …';
}

function unwrapChannelEvent(event) {
  if (!event) return null;
  if (typeof event === 'string') {
    try { return JSON.parse(event); } catch { return null; }
  }
  return typeof event === 'object' ? event : null;
}

function applyHarnessRealtime(payload, channelId) {
  if (!payload?.type?.startsWith('harness.') || !channelId) return;
  const prev = state.harnessByChannel[channelId] || { plan: null, tasks: [] };
  state.harnessByChannel[channelId] = applyHarnessEvent(prev, payload);
  if (state.currentChannel?.id === channelId && elements.harnessOverview) {
    renderHarnessOverview(elements.harnessOverview, state.harnessByChannel[channelId]);
  }
  updateHarnessTeaser(channelId);
  if (activePage() === 'canvas') loadCanvas().catch(() => {});
}

function handleRealtimeMessage(payload) {
  if (!payload?.type) return;
  if (payload.type === EVENT.channelActivity && payload.channel_id) {
    const inner = unwrapChannelEvent(payload.event);
    if (inner?.type?.startsWith('harness.')) {
      applyHarnessRealtime(inner, payload.channel_id);
      return;
    }
    if (state.currentChannel?.id === payload.channel_id) refreshCurrentChannelMessages();
    return;
  }
  if (payload.type === EVENT.overviewStats) {
    if (elements.livePulseAgents) elements.livePulseAgents.textContent = String(payload.active_agents?.total ?? 0);
    if (elements.livePulseProposals) elements.livePulseProposals.textContent = String(payload.pending_proposals ?? 0);
    const statAgents = document.getElementById('statActiveAgents');
    const statProposals = document.getElementById('statPendingProposals');
    if (statAgents) statAgents.textContent = String(payload.active_agents?.total ?? 0);
    if (statProposals) statProposals.textContent = String(payload.pending_proposals ?? 0);
    return;
  }
  if (payload.type === EVENT.canvasEvent) loadCanvas().catch(() => {});
  if (String(payload.type).startsWith('harness.')) {
    applyHarnessRealtime(payload, payload.channel_id || state.currentChannel?.id);
  }
}

async function loadPortalData() {
  const [dashR, skillsR, proposalsR, monR] = await Promise.allSettled([
    fetchJSON('/api/dashboard'),
    fetchJSON('/api/skills'),
    fetchJSON('/api/proposals'),
    fetchJSON('/api/monitoring'),
  ]);
  if (dashR.status === 'fulfilled') renderDashboard(dashR.value);
  if (skillsR.status === 'fulfilled') renderSkills(skillsR.value);
  if (proposalsR.status === 'fulfilled') {
    state.proposals = proposalsR.value;
    renderCourtProposals(proposalsR.value);
  }
  if (monR.status === 'fulfilled') {
    state.monitoring = monR.value;
    renderSystemHealth(monR.value);
  }
  loadSidebarChannels().catch(() => {});
  loadHarness('main').then(() => updateHarnessTeaser('main')).catch(() => {});
}

async function loadHarness(channelId) {
  try {
    const data = await fetchJSON(`/api/channels/${channelId}/harness`);
    state.harnessByChannel[channelId] = data;
    if (state.currentChannel?.id === channelId && elements.harnessOverview) {
      renderHarnessOverview(elements.harnessOverview, data);
    }
    realtime.subscribeChannel(channelId, data?.plan?.plan_id);
    return data;
  } catch {
    return null;
  }
}

function updateHarnessTeaser(channelId = 'main') {
  if (!elements.harnessTeaserGoal) return;
  const data = state.harnessByChannel[channelId];
  const goal = data?.plan?.goal;
  const taskCount = data?.tasks?.length || 0;
  if (goal) {
    elements.harnessTeaserGoal.textContent = taskCount
      ? `${goal} — ${taskCount} active task(s)`
      : goal;
  } else {
    elements.harnessTeaserGoal.textContent = 'No active plan — submit a goal to begin.';
  }
}

async function submitGoal(ev) {
  ev.preventDefault();
  const input = document.getElementById('commandBarInput');
  const goal = (input?.value || '').trim();
  if (!goal) return;
  try {
    const preview = await fetchJSON('/api/goals', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ goal }),
    });
    state.planPreview = preview;
    if (elements.planPreview) {
      elements.planPreview.hidden = false;
      const goalEl = elements.planPreview.querySelector('[data-preview-goal]');
      const chEl = elements.planPreview.querySelector('[data-preview-channel]');
      if (goalEl) goalEl.textContent = preview.goal;
      if (chEl) chEl.textContent = preview.channel_id;
    }
    state.harnessByChannel[preview.channel_id] = {
      plan: { goal: preview.goal, stages: preview.stages, channel_id: preview.channel_id },
      tasks: [],
    };
    updateHarnessTeaser(preview.channel_id);
    if (input) input.value = '';
  } catch (e) {
    alert('Goal submission failed: ' + e.message);
  }
}

function openPlanPreviewChannel() {
  if (!state.planPreview?.channel_id) return;
  location.hash = 'channels';
  fetchJSON('/api/channels').then((data) => {
    const ch = (data.channels || []).find((c) => c.id === state.planPreview.channel_id);
    selectChannel(ch || { id: state.planPreview.channel_id, members: [] });
  }).catch(() => selectChannel({ id: state.planPreview.channel_id, members: [] }));
  if (elements.planPreview) elements.planPreview.hidden = true;
}

function loadSidebarChannels() {
  return fetchJSON('/api/channels').then((data) => {
    renderChannelsList(data.channels || []);
  });
}

function renderChannelsList(chs) {
  const ul = elements.sidebarChannelsList;
  if (!ul) return;
  ul.replaceChildren();
  chs.forEach((ch) => {
    if (ch.archived) return;
    const li = document.createElement('li');
    li.className = 'list-card';
    if (state.currentChannel?.id === ch.id) li.classList.add('active');
    const memCount = (ch.members || []).length;
    li.innerHTML = `<span>${ch.id}</span><small>${memCount} members</small>`;
    li.onclick = () => {
      location.hash = 'channels';
      selectChannel(ch);
    };
    ul.appendChild(li);
  });
}

async function createChannel(ev) {
  ev.preventDefault();
  const id = (elements.newChannelId?.value || '').trim();
  if (!id) return;
  await fetch('/api/channels', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
  if (elements.newChannelId) elements.newChannelId.value = '';
  await loadSidebarChannels();
}

function selectChannel(ch) {
  state.currentChannel = ch;
  if (elements.channelEmptyState) elements.channelEmptyState.style.display = 'none';
  if (elements.channelDetail) elements.channelDetail.style.display = 'grid';
  if (elements.selectedChannelId) elements.selectedChannelId.textContent = ch.id;
  if (elements.channelContextSummary) {
    elements.channelContextSummary.textContent = `Channel ${ch.id} — ${(ch.members || []).length} members`;
  }
  renderMembers(ch.members || []);
  loadSidebarChannels();
  fetchJSON(`/api/channels/${ch.id}`).then((full) => {
    state.currentChannel = full;
    renderChannelMessages(full.messages || []);
    loadHarness(ch.id).then(() => updateHarnessTeaser(ch.id));
  }).catch(() => {
    renderChannelMessages(ch.messages || []);
    loadHarness(ch.id);
  });
}

function showChannelEmpty() {
  state.currentChannel = null;
  if (elements.channelEmptyState) elements.channelEmptyState.style.display = 'block';
  if (elements.channelDetail) elements.channelDetail.style.display = 'none';
}

function renderMembers(members) {
  const groups = groupMembers(members);
  const ul = elements.membersList;
  if (!ul) return;
  ul.replaceChildren();
  Object.entries(groups).forEach(([group, items]) => {
    if (!items.length) return;
    const section = document.createElement('li');
    section.className = 'member-group';
    section.innerHTML = `<strong class="member-group__title">${group}</strong>`;
    const inner = document.createElement('ul');
    inner.className = 'list-stack compact-list';
    items.forEach((m) => {
      const role = m.role || m.agent_id || 'member';
      const li = document.createElement('li');
      li.className = 'list-card member-chip';
      li.dataset.testid = `member-${role}`;
      li.textContent = role;
      const btn = document.createElement('button');
      btn.textContent = 'Remove';
      btn.className = 'tiny-danger';
      btn.onclick = async () => {
        if (!state.currentChannel || !confirm(`Remove ${role}?`)) return;
        await fetch(`/api/channels/${state.currentChannel.id}/members/remove`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-Aegis-Confirmed': '1' },
          body: JSON.stringify({ role }),
        });
        const fresh = await fetchJSON(`/api/channels/${state.currentChannel.id}`);
        state.currentChannel = fresh;
        renderMembers(fresh.members || []);
        loadSidebarChannels();
      };
      li.appendChild(btn);
      inner.appendChild(li);
    });
    section.appendChild(inner);
    ul.appendChild(section);
  });
}

function groupMembers(members) {
  const groups = { 'Core Court': [], 'Project / SDLC': [], Humans: [] };
  (members || []).forEach((m) => {
    const role = m.role || m.agent_id || 'member';
    if (role.startsWith('court-persona-')) groups['Core Court'].push(m);
    else if (role.startsWith('user:') || role === 'user') groups.Humans.push(m);
    else groups['Project / SDLC'].push(m);
  });
  return groups;
}

function formatMessageTime(ts) {
  if (ts == null || ts === '') return '';
  const d = new Date(typeof ts === 'number' ? ts : ts);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString();
}

function renderChannelMessages(messages) {
  const div = document.getElementById('channelMessages');
  if (!div) return;
  div.replaceChildren();
  messages.forEach((m) => {
    const entry = document.createElement('div');
    entry.className = 'message';
    const header = document.createElement('div');
    const who = document.createElement('strong');
    who.textContent = m.from || 'unknown';
    const when = document.createElement('small');
    when.textContent = formatMessageTime(m.ts);
    header.append(who, document.createTextNode(' '), when);
    const body = document.createElement('div');
    body.textContent = typeof m.content === 'string' ? m.content : JSON.stringify(m.content ?? '');
    entry.append(header, body);
    div.appendChild(entry);
  });
  div.scrollTop = div.scrollHeight;
}

async function refreshCurrentChannelMessages() {
  if (!state.currentChannel) return;
  try {
    const fresh = await fetchJSON(`/api/channels/${state.currentChannel.id}`);
    state.currentChannel = fresh;
    renderChannelMessages(fresh.messages || []);
  } catch { /* keep */ }
}

async function postToChannel(ev) {
  ev.preventDefault();
  if (!state.currentChannel) return;
  const from = (document.getElementById('postFrom')?.value || 'user').trim();
  const content = (document.getElementById('postContent')?.value || '').trim();
  if (!content) return;
  await fetch(`/api/channels/${state.currentChannel.id}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from, content }),
  });
  document.getElementById('postContent').value = '';
  await refreshCurrentChannelMessages();
}

async function addMember(ev) {
  ev.preventDefault();
  if (!state.currentChannel) return;
  const role = (elements.newMemberRole?.value || '').trim();
  if (!role) return;
  await fetch(`/api/channels/${state.currentChannel.id}/members`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ role }),
  });
  if (elements.newMemberRole) elements.newMemberRole.value = '';
  if (elements.addMemberForm) elements.addMemberForm.hidden = true;
  const fresh = await fetchJSON(`/api/channels/${state.currentChannel.id}`);
  state.currentChannel = fresh;
  renderMembers(fresh.members || []);
  loadSidebarChannels();
}

async function archiveChannel() {
  if (!state.currentChannel || !confirm('Archive this channel?')) return;
  await fetch(`/api/channels/${state.currentChannel.id}/archive`, {
    method: 'POST',
    headers: { 'X-Aegis-Confirmed': '1' },
  });
  showChannelEmpty();
  await loadSidebarChannels();
}

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(options.headers || {}) },
    ...options,
  });
  if (!response.ok) throw new Error(`Request failed: ${url}`);
  return response.json();
}

function renderDashboard(data) {
  if (!elements.systemStatusLabel) return;
  elements.systemStatusLabel.textContent = titleCase(data.system_status);
  elements.runtimeLabel.textContent = data.runtime;
  if (elements.notificationCount) elements.notificationCount.textContent = String(data.notifications);
  if (elements.safeModeLabel) elements.safeModeLabel.textContent = data.safe_mode ? 'ON' : 'OFF';
  const settingsSafe = document.getElementById('settingsSafeModeLabel');
  if (settingsSafe) settingsSafe.textContent = data.safe_mode ? 'ON' : 'OFF';
  document.getElementById('statActiveAgents').textContent = String(data.quick_stats.active_agents);
  document.getElementById('statBackgroundTasks').textContent = String(data.quick_stats.background_tasks);
  document.getElementById('statSkillsInstalled').textContent = String(data.quick_stats.skills_installed);
  document.getElementById('statPendingProposals').textContent = String(data.quick_stats.pending_proposals);
  document.getElementById('statChannels').textContent = String(data.channel_count || data.quick_stats?.channel_count || 0);
  if (elements.livePulseAgents) elements.livePulseAgents.textContent = String(data.quick_stats.active_agents);
  if (elements.livePulseProposals) elements.livePulseProposals.textContent = String(data.quick_stats.pending_proposals);
  renderList(elements.recentActivityList, data.recent_activity, (entry) => buildListCard(entry, 'Recent activity'));
  renderHomeRecent(data);
  state.activeWork = data.active_work || [];
  refreshActiveWorkPanel();
  if (state.monitoring) renderSystemHealth(state.monitoring);
}

function renderHomeRecent(data) {
  const items = [];
  (data.active_work || []).slice(0, 3).forEach((w) => {
    items.push({ title: w.scope || w.persona || 'Task', subtitle: `${w.stage || '—'} • ${w.channel_id || 'main'}` });
  });
  (data.recent_activity || []).slice(0, 3 - items.length).forEach((a) => {
    items.push({ title: String(a), subtitle: 'Activity' });
  });
  renderList(elements.homeRecentList, items, (item) => buildListCard(item.title, item.subtitle));
}

function renderSystemHealth(monitoring) {
  const el = (id, val) => { const n = document.getElementById(id); if (n) n.textContent = val; };
  el('statRunningVMs', String(monitoring.stats?.running_vms ?? 0));
  el('statCPUUsage', monitoring.stats?.cpu_usage ?? '—');
  el('statMemoryUsage', monitoring.stats?.memory_usage ?? '—');
}

function refreshActiveWorkPanel() {
  const filtered = filterActiveWork(state.activeWork, state.dashboardFilter);
  renderActiveWork(elements.activeWorkList, filtered, {
    onPause: (item) => agentIntervention(item.id, 'pause'),
    onTrace: (item) => openTrace(item.id || item.persona),
    onCanvas: () => { location.hash = 'canvas'; loadCanvas(); },
    onCourt: (item) => {
      location.hash = 'court';
      const p = state.proposals.find((x) => x.id === item.proposal_id);
      if (p) selectProposal(p);
    },
  });
}

async function loadCanvas() {
  const channelId = state.currentChannel?.id || 'main';
  const data = await fetchJSON(`/api/canvas?channel_id=${encodeURIComponent(channelId)}`);
  renderCanvas(elements.canvasRoot, data, {
    onChannelLink: (id) => {
      location.hash = 'channels';
      fetchJSON('/api/channels').then((resp) => {
        const ch = (resp.channels || []).find((c) => c.id === id);
        if (ch) selectChannel(ch);
        else selectChannel({ id, members: [] });
      }).catch(() => selectChannel({ id, members: [] }));
    },
  });
}

async function loadAgents() {
  const data = await fetchJSON('/api/agents');
  renderList(elements.agentsList, data.agents || [], (agent) => {
    const card = buildListCard(agent.name, `${agent.status} • ${agent.task}`);
    card.style.cursor = 'pointer';
    card.onclick = () => openTrace(agent.name);
    return card;
  });
}

function openTrace(agentId) {
  state.traceAgentId = agentId;
  location.hash = `trace?agent=${encodeURIComponent(agentId)}`;
  loadTrace(agentId);
}

async function loadTrace(agentId) {
  const id = agentId || state.traceAgentId || 'researcher';
  document.getElementById('traceAgentTitle').textContent = `Trace: ${id}`;
  document.getElementById('currentAgentName').textContent = id;
  document.getElementById('currentTraceId').textContent = id;
  const data = await fetchJSON(`/api/agents/${encodeURIComponent(id)}/trace`);
  renderTrace(elements.traceTimeline, data);
}

async function agentIntervention(agentId, action) {
  if (!confirm(`Confirm ${action} for ${agentId}?`)) return;
  await fetch(`/api/agents/${encodeURIComponent(agentId)}/${action}`, {
    method: 'POST',
    headers: { 'X-Aegis-Confirmed': '1' },
  });
  await loadPortalData();
}

function renderCourtProposals(proposals) {
  renderProposalList(elements.proposalsList, proposals, { onSelect: selectProposal });
}

async function selectProposal(proposal) {
  state.selectedProposal = proposal;
  if (elements.courtDetail) elements.courtDetail.hidden = false;
  const detail = await fetchJSON(`/api/proposals/${proposal.id}/reviews`);
  renderProposalDetail(elements.courtDetail, detail);
  wireCourtActions(proposal.id);
}

function wireCourtActions(proposalId) {
  const root = elements.courtDetail;
  if (!root) return;
  root.querySelectorAll('[data-action]').forEach((btn) => {
    btn.onclick = async () => {
      const action = btn.dataset.action;
      if (action === 'export') {
        window.open(`/api/proposals/${proposalId}/audit`, '_blank');
        return;
      }
      try {
        await proposalAction(proposalId, action);
        await loadPortalData();
        const p = state.proposals.find((x) => x.id === proposalId);
        if (p) await selectProposal(p);
      } catch (e) {
        alert(e.message);
      }
    };
  });
}

function renderSkills(skills) {
  if (!elements.skillsList) return;
  elements.skillsList.replaceChildren(...skills.map((skill) => {
    const article = document.createElement('article');
    article.className = 'subpanel';
    article.dataset.testid = `skill-${skill.id}`;
    article.innerHTML = `<div class="subpanel-header"><h3>${skill.name} (${skill.version})</h3><span class="subtle">${skill.status}</span></div><p>${skill.description}</p>`;
    return article;
  }));
}

function renderList(target, items, mapper) {
  if (!target) return;
  target.replaceChildren(...(items || []).map(mapper));
}

function buildListCard(title, subtitle) {
  const item = document.createElement('li');
  item.className = 'list-card';
  const strong = document.createElement('span');
  strong.textContent = title;
  const small = document.createElement('small');
  small.textContent = subtitle;
  item.append(strong, small);
  return item;
}

function titleCase(value) {
  return String(value).replace(/[_-]+/g, ' ').replace(/\b\w/g, (m) => m.toUpperCase());
}

function activePage() {
  const raw = location.hash.slice(1);
  let page = raw.split('?')[0];
  if (page === 'monitoring') page = 'dashboard';
  if (page === 'trace') {
    const params = new URLSearchParams(raw.split('?')[1] || '');
    const agent = params.get('agent');
    if (agent) state.traceAgentId = agent;
  }
  return Object.prototype.hasOwnProperty.call(PAGE_TITLES, page) ? page : 'home';
}

function applyLayout(page) {
  const grid = elements.workspaceGrid;
  if (!grid) return;
  const layout = LAYOUT_BY_PAGE[page] || 'layout--main';
  grid.className = `workspace-grid ${layout}`;
  if (sessionStorage.getItem('sidebarCollapsed') === '1') grid.classList.add('sidebar-collapsed');
  if (sessionStorage.getItem('contextCollapsed') === '1') grid.classList.add('context-collapsed');

  document.querySelectorAll('[data-context-mode]').forEach((el) => {
    const mode = el.dataset.contextMode;
    el.hidden = !(
      (page === 'home' && mode === 'home') ||
      (page === 'channels' && mode === 'channels') ||
      (page === 'trace' && mode === 'trace')
    );
  });

  const titles = { home: 'Overview', channels: 'Channel', trace: 'Agent Trace' };
  const titleEl = document.getElementById('contextPanelTitle');
  if (titleEl) titleEl.textContent = titles[page] || 'Context';
}

function navigate(page) {
  if (page === 'monitoring') {
    location.hash = 'dashboard';
    return;
  }
  const safePage = Object.prototype.hasOwnProperty.call(PAGE_TITLES, page) ? page : 'home';
  document.querySelectorAll('[data-page]').forEach((panel) => {
    panel.hidden = panel.dataset.page !== safePage && !(safePage === 'dashboard' && panel.dataset.page === 'monitoring');
  });
  document.querySelectorAll('[data-nav-page]').forEach((btn) => {
    btn.classList.toggle('is-active', btn.dataset.navPage === safePage);
  });
  document.title = `${PAGE_TITLES[safePage]} — AegisClaw Secure Command Center`;
  applyLayout(safePage);

  const channelId = state.currentChannel?.id;
  const planId = state.harnessByChannel[channelId]?.plan?.plan_id
    || state.harnessByChannel.main?.plan?.plan_id;
  realtime.setViewTopics(safePage, {
    channelId: safePage === 'canvas' ? (channelId || 'main') : channelId,
    planId,
    proposalId: state.selectedProposal?.id,
  });

  if (safePage === 'channels' && !state.currentChannel) showChannelEmpty();
  if (safePage === 'canvas') loadCanvas().catch(() => {});
  if (safePage === 'agents') loadAgents().catch(() => {});
  if (safePage === 'trace' && state.traceAgentId) loadTrace(state.traceAgentId).catch(() => {});
}

function wireRouter() {
  document.querySelectorAll('[data-nav-page]').forEach((button) => {
    button.addEventListener('click', () => {
      location.hash = button.dataset.navPage;
    });
  });
  window.addEventListener('hashchange', () => navigate(activePage()));
}

function wireQuickStarts() {
  document.querySelectorAll('[data-quick-start]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const input = document.getElementById('commandBarInput');
      if (input) {
        input.value = btn.dataset.quickStart || '';
        input.focus();
      }
    });
  });
}

function wireLayoutToggles() {
  document.getElementById('toggleSidebarBtn')?.addEventListener('click', () => {
    const grid = elements.workspaceGrid;
    const collapsed = grid?.classList.toggle('sidebar-collapsed');
    sessionStorage.setItem('sidebarCollapsed', collapsed ? '1' : '0');
  });
  document.getElementById('toggleContextBtn')?.addEventListener('click', () => {
    const grid = elements.workspaceGrid;
    const collapsed = grid?.classList.toggle('context-collapsed');
    sessionStorage.setItem('contextCollapsed', collapsed ? '1' : '0');
  });
  document.getElementById('toggleInviteBtn')?.addEventListener('click', () => {
    if (elements.addMemberForm) elements.addMemberForm.hidden = !elements.addMemberForm.hidden;
  });
}

async function boot() {
  document.body.dataset.portalReady = '1';
  wireRouter();
  wireQuickStarts();
  wireLayoutToggles();
  if (elements.newChannelForm) elements.newChannelForm.addEventListener('submit', createChannel);
  if (elements.addMemberForm) elements.addMemberForm.addEventListener('submit', addMember);
  if (elements.archiveChannelBtn) elements.archiveChannelBtn.addEventListener('click', archiveChannel);
  document.getElementById('channelPostForm')?.addEventListener('submit', postToChannel);
  document.getElementById('commandBarForm')?.addEventListener('submit', submitGoal);
  document.getElementById('planPreviewOpen')?.addEventListener('click', openPlanPreviewChannel);
  document.querySelector('[data-testid="new-channel-button"]')?.addEventListener('click', () => {
    location.hash = 'channels';
    elements.newChannelId?.focus();
  });
  document.getElementById('dashboardFilter')?.addEventListener('change', (ev) => {
    state.dashboardFilter = ev.target.value;
    refreshActiveWorkPanel();
  });
  document.querySelectorAll('[data-trace-action]').forEach((btn) => {
    btn.addEventListener('click', () => {
      if (state.traceAgentId) agentIntervention(state.traceAgentId, btn.dataset.traceAction);
    });
  });
  if (!location.hash) location.hash = 'home';
  navigate(activePage());
  realtime.connect();
  try {
    await loadPortalData();
  } catch {
    console.log('Portal partial load');
  }
}

boot();