import { EVENT } from './js/contracts.js';
import { RealtimeClient } from './js/realtime.js';
import { renderHarnessOverview, applyHarnessEvent } from './js/harness.js';

const state = {
  harnessByChannel: {},
  currentChannel: null,
  planPreview: null,
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
  monitoring: 'Monitoring',
  teams: 'Team Workspace',
};

const elements = {
  systemStatusLabel: document.getElementById('systemStatusLabel'),
  runtimeLabel: document.getElementById('runtimeLabel'),
  notificationCount: document.getElementById('notificationCount'),
  safeModeLabel: document.getElementById('safeModeLabel'),
  connectionLabel: document.getElementById('connectionStatusLabel'),
  activeAgentsList: document.getElementById('activeAgentsList'),
  recentActivityList: document.getElementById('recentActivityList'),
  skillsList: document.getElementById('skillsList'),
  proposalsList: document.getElementById('proposalsList'),
  monitoringAgentsList: document.getElementById('monitoringAgentsList'),
  monitoringLogs: document.getElementById('monitoringLogs'),
  channelsList: document.getElementById('channelsList'),
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
};

const realtime = new RealtimeClient({
  onMessage: handleRealtimeMessage,
  onStatus: updateConnectionStatus,
});

function updateConnectionStatus(mode) {
  if (!elements.connectionLabel) return;
  const labels = {
    stomp: 'Conn STOMP',
    'sse-fallback': 'Conn SSE',
    disconnected: 'Conn Off',
  };
  elements.connectionLabel.textContent = labels[mode] || 'Conn …';
}

function handleRealtimeMessage(payload) {
  if (!payload?.type) return;
  if (payload.type === EVENT.channelActivity && payload.channel_id) {
    if (state.currentChannel?.id === payload.channel_id) {
      refreshCurrentChannelMessages();
    }
    return;
  }
  if (payload.type === EVENT.overviewStats) {
    if (elements.livePulseAgents) {
      elements.livePulseAgents.textContent = String(payload.active_agents?.total ?? 0);
    }
    if (elements.livePulseProposals) {
      elements.livePulseProposals.textContent = String(payload.pending_proposals ?? 0);
    }
    return;
  }
  if (String(payload.type).startsWith('harness.')) {
    const chId = payload.channel_id || state.currentChannel?.id;
    if (chId) {
      const prev = state.harnessByChannel[chId] || { plan: null, tasks: [] };
      state.harnessByChannel[chId] = applyHarnessEvent(prev, payload);
      if (state.currentChannel?.id === chId && elements.harnessOverview) {
        renderHarnessOverview(elements.harnessOverview, state.harnessByChannel[chId]);
      }
    }
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
  if (proposalsR.status === 'fulfilled') renderProposals(proposalsR.value);
  if (monR.status === 'fulfilled') renderMonitoring(monR.value);
  loadChannelsForUI().catch(() => {});
  loadSidebarChannels().catch(() => {});
}

async function loadHarness(channelId) {
  try {
    const data = await fetchJSON(`/api/channels/${channelId}/harness`);
    state.harnessByChannel[channelId] = data;
    if (state.currentChannel?.id === channelId && elements.harnessOverview) {
      renderHarnessOverview(elements.harnessOverview, data);
    }
    const planId = data?.plan?.plan_id;
    realtime.subscribeChannel(channelId, planId);
  } catch {
    /* harness optional on cold boot */
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
      renderHarnessOverview(elements.planPreview.querySelector('[data-harness-preview]'), {
        plan: { goal: preview.goal, stages: preview.stages },
        tasks: [],
      });
    }
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
    if (ch) selectChannel(ch);
    else selectChannel({ id: state.planPreview.channel_id, members: [] });
  }).catch(() => {
    selectChannel({ id: state.planPreview.channel_id, members: [] });
  });
  if (elements.planPreview) elements.planPreview.hidden = true;
}

async function loadChannelsForUI() {
  const data = await fetchJSON('/api/channels');
  renderChannelsList(data.channels || []);
}

function renderChannelsList(chs) {
  const ul = elements.channelsList;
  if (!ul) return;
  ul.innerHTML = '';
  chs.forEach((ch) => {
    if (ch.archived) return;
    const li = document.createElement('li');
    li.className = 'list-card';
    const memCount = (ch.members || []).length;
    li.innerHTML = `<span>${ch.id}</span><small>${memCount} members</small>`;
    li.onclick = () => selectChannel(ch);
    ul.appendChild(li);
  });
}

function loadSidebarChannels() {
  fetchJSON('/api/channels').then((data) => {
    const ul = document.getElementById('sidebarChannelsList');
    if (!ul) return;
    ul.innerHTML = '';
    (data.channels || []).forEach((ch) => {
      if (ch.archived) return;
      const li = document.createElement('li');
      li.className = 'list-card';
      const memCount = (ch.members || []).length;
      li.innerHTML = `<span>${ch.id}</span><small>${memCount} members</small>`;
      li.onclick = () => {
        location.hash = 'channels';
        selectChannel(ch);
      };
      ul.appendChild(li);
    });
  }).catch(() => {});
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
  await loadChannelsForUI();
}

function selectChannel(ch) {
  state.currentChannel = ch;
  if (elements.selectedChannelId) elements.selectedChannelId.textContent = ch.id;
  if (elements.channelDetail) elements.channelDetail.style.display = 'block';
  renderMembers(ch.members || []);
  fetchJSON(`/api/channels/${ch.id}`).then((full) => {
    state.currentChannel = full;
    renderChannelMessages(full.messages || []);
    loadHarness(ch.id);
  }).catch(() => {
    renderChannelMessages(ch.messages || []);
    loadHarness(ch.id);
  });
}

function renderMembers(members) {
  const groups = groupMembers(members);
  const ul = elements.membersList;
  if (!ul) return;
  ul.replaceChildren();
  Object.entries(groups).forEach(([group, items]) => {
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
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ role }),
        });
        const fresh = await fetchJSON(`/api/channels/${state.currentChannel.id}`);
        state.currentChannel = fresh;
        renderMembers(fresh.members || []);
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
  if (typeof ts === 'number') return new Date(ts).toLocaleString();
  const d = new Date(ts);
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
  } catch {
    /* keep last rendered */
  }
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
  const fresh = await fetchJSON(`/api/channels/${state.currentChannel.id}`);
  state.currentChannel = fresh;
  renderMembers(fresh.members || []);
}

async function archiveChannel() {
  if (!state.currentChannel || !confirm('Archive this channel?')) return;
  await fetch(`/api/channels/${state.currentChannel.id}/archive`, { method: 'POST' });
  if (elements.channelDetail) elements.channelDetail.style.display = 'none';
  state.currentChannel = null;
  await loadChannelsForUI();
}

async function fetchJSON(url, options) {
  const response = await fetch(url, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
    ...options,
  });
  if (!response.ok) throw new Error(`Request failed: ${url}`);
  return response.json();
}

function renderDashboard(data) {
  if (!elements.systemStatusLabel) return;
  elements.systemStatusLabel.textContent = `Daemon ${titleCase(data.system_status)}`;
  elements.runtimeLabel.textContent = data.runtime;
  elements.notificationCount.textContent = String(data.notifications);
  elements.safeModeLabel.textContent = data.safe_mode ? 'ON' : 'OFF';
  document.getElementById('statActiveAgents').textContent = String(data.quick_stats.active_agents);
  document.getElementById('statBackgroundTasks').textContent = String(data.quick_stats.background_tasks);
  document.getElementById('statSkillsInstalled').textContent = String(data.quick_stats.skills_installed);
  document.getElementById('statPendingProposals').textContent = String(data.quick_stats.pending_proposals);
  const chCount = data.channel_count || data.quick_stats?.channel_count || 0;
  document.getElementById('statChannels').textContent = String(chCount);
  if (elements.livePulseAgents) elements.livePulseAgents.textContent = String(data.quick_stats.active_agents);
  if (elements.livePulseProposals) elements.livePulseProposals.textContent = String(data.quick_stats.pending_proposals);
  renderList(elements.activeAgentsList, data.agents, (agent) =>
    buildListCard(agent.name, `${titleCase(agent.status)} • ${agent.task} (${agent.progress})`),
  );
  renderList(elements.recentActivityList, data.recent_activity, (entry) => buildListCard(entry, 'Recent audited activity'));
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

function renderProposals(proposals) {
  if (!elements.proposalsList) return;
  elements.proposalsList.replaceChildren(...proposals.map((proposal) => {
    const article = document.createElement('article');
    article.className = 'subpanel';
    article.dataset.testid = `proposal-${proposal.id}`;
    article.innerHTML = `<div class="subpanel-header"><h3>${proposal.title}</h3><span class="subtle">${proposal.status}</span></div><p>${proposal.summary} Votes: ${proposal.votes}.</p>`;
    return article;
  }));
}

function renderMonitoring(monitoring) {
  document.getElementById('statRunningVMs').textContent = String(monitoring.stats.running_vms);
  document.getElementById('statMonitoringTasks').textContent = String(monitoring.stats.background_tasks);
  document.getElementById('statCPUUsage').textContent = monitoring.stats.cpu_usage;
  document.getElementById('statMemoryUsage').textContent = monitoring.stats.memory_usage;
  renderList(elements.monitoringAgentsList, monitoring.agents, (agent) =>
    buildListCard(agent.name, `${agent.status} • ${agent.progress}`),
  );
  if (elements.monitoringLogs) elements.monitoringLogs.textContent = monitoring.logs.join('\n');
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
  const hash = location.hash.slice(1).split('?')[0];
  return Object.prototype.hasOwnProperty.call(PAGE_TITLES, hash) ? hash : 'home';
}

function navigate(page) {
  const safePage = Object.prototype.hasOwnProperty.call(PAGE_TITLES, page) ? page : 'home';
  document.querySelectorAll('[data-page]').forEach((panel) => {
    panel.hidden = panel.dataset.page !== safePage;
  });
  document.querySelectorAll('[data-nav-page]').forEach((btn) => {
    btn.classList.toggle('is-active', btn.dataset.navPage === safePage);
  });
  document.title = `${PAGE_TITLES[safePage]} — AegisClaw Secure Command Center`;
  realtime.setViewTopics(safePage, {
    channelId: state.currentChannel?.id,
    planId: state.harnessByChannel[state.currentChannel?.id]?.plan?.plan_id,
  });
}

function wireRouter() {
  document.querySelectorAll('[data-nav-page]').forEach((button) => {
    button.addEventListener('click', () => {
      location.hash = button.dataset.navPage;
    });
  });
  window.addEventListener('hashchange', () => navigate(activePage()));
}

async function boot() {
  wireRouter();
  if (elements.newChannelForm) elements.newChannelForm.addEventListener('submit', createChannel);
  if (elements.addMemberForm) elements.addMemberForm.addEventListener('submit', addMember);
  if (elements.archiveChannelBtn) elements.archiveChannelBtn.addEventListener('click', archiveChannel);
  document.getElementById('channelPostForm')?.addEventListener('submit', postToChannel);
  document.getElementById('commandBarForm')?.addEventListener('submit', submitGoal);
  document.getElementById('planPreviewOpen')?.addEventListener('click', openPlanPreviewChannel);
  document.querySelector('[data-testid="new-channel-button"]')?.addEventListener('click', () => {
    location.hash = 'channels';
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