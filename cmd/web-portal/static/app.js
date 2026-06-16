const state = {
  sessionId: `sess_${Date.now()}`,
  eventSource: null,
  currentAgentNode: null,
  currentTraceId: 'waiting',
  recentTools: [],
};

const PAGE_TITLES = {
  dashboard: 'Dashboard',
  channels: 'Channels',
  teams: 'Team Workspace',
  agents: 'Agents',
  skills: 'Skills Registry',
  court: 'Court / Governance',
  monitoring: 'Monitoring',
  audit: 'Audit Log',
};

const elements = {
  systemStatusLabel: document.getElementById('systemStatusLabel'),
  runtimeLabel: document.getElementById('runtimeLabel'),
  notificationCount: document.getElementById('notificationCount'),
  safeModeLabel: document.getElementById('safeModeLabel'),
  activeAgentsList: document.getElementById('activeAgentsList'),
  recentActivityList: document.getElementById('recentActivityList'),
  skillsList: document.getElementById('skillsList'),
  proposalsList: document.getElementById('proposalsList'),
  monitoringAgentsList: document.getElementById('monitoringAgentsList'),
  monitoringLogs: document.getElementById('monitoringLogs'),
  currentAgentName: document.getElementById('currentAgentName'),
  currentTraceId: document.getElementById('currentTraceId'),
  recentToolsList: document.getElementById('recentToolsList'),
  // channels UI (replaced chat)
  channelsList: document.getElementById('channelsList'),
  newChannelForm: document.getElementById('newChannelForm'),
  newChannelId: document.getElementById('newChannelId'),
  channelDetail: document.getElementById('channelDetail'),
  selectedChannelId: document.getElementById('selectedChannelId'),
  membersList: document.getElementById('membersList'),
  addMemberForm: document.getElementById('addMemberForm'),
  newMemberRole: document.getElementById('newMemberRole'),
  archiveChannelBtn: document.getElementById('archiveChannelBtn'),
};

async function loadPortalData() {
  // Use allSettled so a failing skills/proposals/dashboard endpoint does not block
  // channels load (core collab path). Partial API failures are common on cold boots.
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

  // Channels are the collaboration backbone; always attempt regardless of other panels.
  loadChannelsForUI().catch(() => {});
  loadSidebarChannels().catch(() => {});
}

let currentChannel = null;
let stompWS = null;
let stompSubscribedChannel = null;

function stompConnect() {
  if (stompWS && (stompWS.readyState === WebSocket.OPEN || stompWS.readyState === WebSocket.CONNECTING)) {
    return;
  }
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  stompWS = new WebSocket(`${proto}//${location.host}/stomp`);
  stompWS.onopen = () => {
    stompWS.send('CONNECT\naccept-version:1.2\n\n\x00');
  };
  stompWS.onmessage = (ev) => {
    const raw = String(ev.data || '');
    if (raw.startsWith('CONNECTED')) {
      if (currentChannel) subscribeChannelSTOMP(currentChannel.id);
      return;
    }
    if (raw.startsWith('MESSAGE')) {
      refreshCurrentChannelMessages();
    }
  };
  stompWS.onclose = () => {
    setTimeout(stompConnect, 3000);
  };
}

function subscribeChannelSTOMP(channelId) {
  if (!stompWS || stompWS.readyState !== WebSocket.OPEN || !channelId) return;
  if (stompSubscribedChannel && stompSubscribedChannel !== channelId) {
    stompWS.send(`UNSUBSCRIBE\nid:sub-${stompSubscribedChannel}\n\n\x00`);
  }
  stompSubscribedChannel = channelId;
  stompWS.send(`SUBSCRIBE\nid:sub-${channelId}\ndestination:/topic/channels.${channelId}.messages\n\n\x00`);
}

async function refreshCurrentChannelMessages() {
  if (!currentChannel) return;
  try {
    const fresh = await fetchJSON(`/api/channels/${currentChannel.id}`);
    currentChannel = fresh;
    renderChannelMessages(fresh.messages || []);
  } catch (_) {
    // keep last rendered messages
  }
}

async function loadChannelsForUI() {
  try {
    const data = await fetchJSON('/api/channels');
    const chs = data.channels || [];
    renderChannelsList(chs);
  } catch (e) {
    console.warn('channels load failed', e);
  }
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
  fetchJSON('/api/channels').then(data => {
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
        // simple: after nav, the page load will handle; for demo, user can click in main list
      };
      ul.appendChild(li);
    });
  }).catch(() => {});
}

async function createChannel(ev) {
  ev.preventDefault();
  const idEl = elements.newChannelId;
  const id = (idEl && idEl.value || '').trim();
  if (!id) return;
  try {
    await fetch('/api/channels', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    });
    if (idEl) idEl.value = '';
    await loadChannelsForUI();
  } catch (e) {
    alert('Create channel failed: ' + e);
  }
}

function selectChannel(ch) {
  currentChannel = ch;
  if (elements.selectedChannelId) elements.selectedChannelId.textContent = ch.id;
  if (elements.channelDetail) elements.channelDetail.style.display = 'block';
  renderMembers(ch.members || []);
  // fetch full for messages
  fetchJSON(`/api/channels/${ch.id}`).then(full => {
    currentChannel = full;
    renderChannelMessages(full.messages || []);
    subscribeChannelSTOMP(full.id);
  }).catch(() => {
    renderChannelMessages(ch.messages || []);
    subscribeChannelSTOMP(ch.id);
  });
}

function renderMembers(members) {
  const ul = elements.membersList;
  if (!ul) return;
  ul.innerHTML = '';
  members.forEach((m) => {
    const li = document.createElement('li');
    const role = m.role || m.agent_id || 'member';
    li.textContent = role;
    const btn = document.createElement('button');
    btn.textContent = 'Remove';
    btn.className = 'tiny-danger';
    btn.onclick = async () => {
      if (!currentChannel) return;
      try {
        await fetch(`/api/channels/${currentChannel.id}/members/remove`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ role }),
        });
        const fresh = await fetchJSON(`/api/channels/${currentChannel.id}`);
        currentChannel = fresh;
        renderMembers(currentChannel.members || []);
      } catch (e) {
        alert('Remove failed: ' + e);
      }
    };
    li.appendChild(btn);
    ul.appendChild(li);
  });
}

function renderChannelMessages(messages) {
  const div = document.getElementById('channelMessages');
  if (!div) return;
  div.innerHTML = '';
  messages.forEach((m) => {
    const entry = document.createElement('div');
    entry.className = 'message';
    const ts = m.ts ? new Date(m.ts / 1000000).toLocaleTimeString() : '';
    entry.innerHTML = `<strong>${m.from || 'unknown'}</strong> <small>${ts}</small><br>${m.content || ''}`;
    div.appendChild(entry);
  });
  div.scrollTop = div.scrollHeight;
}

async function postToChannel(ev) {
  ev.preventDefault();
  if (!currentChannel) return;
  const from = (document.getElementById('postFrom') && document.getElementById('postFrom').value || 'user').trim();
  const content = (document.getElementById('postContent') && document.getElementById('postContent').value || '').trim();
  if (!content) return;
  try {
    await fetch(`/api/channels/${currentChannel.id}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from, content }),
    });
    if (document.getElementById('postContent')) document.getElementById('postContent').value = '';
    // refresh messages
    const fresh = await fetchJSON(`/api/channels/${currentChannel.id}`);
    currentChannel = fresh;
    renderChannelMessages(fresh.messages || []);
  } catch (e) {
    alert('Post failed: ' + e);
  }
}

async function addMember(ev) {
  ev.preventDefault();
  if (!currentChannel) return;
  const role = (elements.newMemberRole && elements.newMemberRole.value || '').trim();
  if (!role) return;
  try {
    await fetch(`/api/channels/${currentChannel.id}/members`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role }),
    });
    if (elements.newMemberRole) elements.newMemberRole.value = '';
    const fresh = await fetchJSON(`/api/channels/${currentChannel.id}`);
    currentChannel = fresh;
    renderMembers(currentChannel.members || []);
  } catch (e) {
    alert('Add participant failed: ' + e);
  }
}

async function archiveChannel() {
  if (!currentChannel) return;
  if (!confirm('Archive this channel?')) return;
  try {
    await fetch(`/api/channels/${currentChannel.id}/archive`, { method: 'POST' });
    if (elements.channelDetail) elements.channelDetail.style.display = 'none';
    currentChannel = null;
    await loadChannelsForUI();
  } catch (e) {
    alert('Archive failed: ' + e);
  }
}

async function fetchJSON(url) {
  const response = await fetch(url, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (!response.ok) {
    throw new Error(`Request failed: ${url}`);
  }
  return response.json();
}

function renderDashboard(data) {
  elements.systemStatusLabel.textContent = `Daemon ${titleCase(data.system_status)}`;
  elements.runtimeLabel.textContent = data.runtime;
  elements.notificationCount.textContent = String(data.notifications);
  elements.safeModeLabel.textContent = data.safe_mode ? 'ON' : 'OFF';

  document.getElementById('statActiveAgents').textContent = String(data.quick_stats.active_agents);
  document.getElementById('statBackgroundTasks').textContent = String(data.quick_stats.background_tasks);
  document.getElementById('statSkillsInstalled').textContent = String(data.quick_stats.skills_installed);
  document.getElementById('statPendingProposals').textContent = String(data.quick_stats.pending_proposals);
  const chCount = data.channel_count || (data.quick_stats && data.quick_stats.channel_count) || 0;
  document.getElementById('statChannels').textContent = String(chCount);

  renderList(elements.activeAgentsList, data.agents, (agent) => {
    const card = buildListCard(agent.name, `${titleCase(agent.status)} • ${agent.task} (${agent.progress})`);
    return card;
  });

  renderList(elements.recentActivityList, data.recent_activity, (entry) => buildListCard(entry, 'Recent audited activity'));
}

function renderSkills(skills) {
  elements.skillsList.replaceChildren(...skills.map((skill) => {
    const article = document.createElement('article');
    article.className = 'subpanel';
    article.dataset.testid = `skill-${skill.id}`;

    const header = document.createElement('div');
    header.className = 'subpanel-header';
    const title = document.createElement('h3');
    title.textContent = `${skill.name} (${skill.version})`;
    const status = document.createElement('span');
    status.className = 'subtle';
    status.textContent = skill.status;
    header.append(title, status);

    const description = document.createElement('p');
    description.textContent = skill.description;

    const details = document.createElement('ul');
    details.className = 'list-stack compact-list';
    details.append(
      buildListCard('Scopes', skill.required_scopes.join(', ') || 'None'),
      buildListCard('Secrets', skill.secrets.join(', ') || 'None'),
    );

    article.append(header, description, details);
    return article;
  }));
}

function renderProposals(proposals) {
  elements.proposalsList.replaceChildren(...proposals.map((proposal) => {
    const article = document.createElement('article');
    article.className = 'subpanel';
    article.dataset.testid = `proposal-${proposal.id}`;

    const header = document.createElement('div');
    header.className = 'subpanel-header';
    const title = document.createElement('h3');
    title.textContent = proposal.title;
    const status = document.createElement('span');
    status.className = 'subtle';
    status.textContent = proposal.status;
    header.append(title, status);

    const summary = document.createElement('p');
    summary.textContent = `${proposal.summary} Votes: ${proposal.votes}.`;

    const gates = document.createElement('ul');
    gates.className = 'list-stack compact-list';
    proposal.security_gates.forEach((gate) => gates.appendChild(buildListCard(gate, 'Court-visible gate status')));

    article.append(header, summary, gates);
    return article;
  }));
}

function renderMonitoring(monitoring) {
  document.getElementById('statRunningVMs').textContent = String(monitoring.stats.running_vms);
  document.getElementById('statMonitoringTasks').textContent = String(monitoring.stats.background_tasks);
  document.getElementById('statCPUUsage').textContent = monitoring.stats.cpu_usage;
  document.getElementById('statMemoryUsage').textContent = monitoring.stats.memory_usage;

  renderList(elements.monitoringAgentsList, monitoring.agents, (agent) => buildListCard(agent.name, `${agent.status} • ${agent.progress}`));
  elements.monitoringLogs.textContent = monitoring.logs.join('\n');
}

function renderList(target, items, mapper) {
  target.replaceChildren(...items.map(mapper));
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

// (Old chat streaming, full markdown renderer, appendMessage, handleStream etc. completely removed
// as part of replacing the chat page/panel with channels as the primary collaboration view per plan Phase 4/5.
// Channels use lightweight renderChannelMessages + /api/channels post. No elements.messages, no streaming, no renderSafeMarkdown remain.)



function titleCase(value) {
  return String(value)
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (match) => match.toUpperCase());
}

function activePage() {
  // Strip any query params from the hash before looking up the page.
  const hash = location.hash.slice(1).split('?')[0];
  return Object.prototype.hasOwnProperty.call(PAGE_TITLES, hash) ? hash : 'dashboard';
}

function navigate(page) {
  // Validate against the known-page whitelist; default to dashboard for unknown values.
  const safePage = Object.prototype.hasOwnProperty.call(PAGE_TITLES, page) ? page : 'dashboard';
  document.querySelectorAll('[data-page]').forEach((panel) => {
    const active = panel.dataset.page === safePage;
    panel.hidden = !active;
  });
  document.querySelectorAll('[data-nav-page]').forEach((btn) => {
    btn.classList.toggle('is-active', btn.dataset.navPage === safePage);
  });
  const title = PAGE_TITLES[safePage];
  document.title = `${title} — AegisClaw Secure Command Center`;
}

function wireRouter() {
  document.querySelectorAll('[data-nav-page]').forEach((button) => {
    button.addEventListener('click', () => {
      const page = button.dataset.navPage;
      location.hash = page;
    });
  });
  window.addEventListener('hashchange', () => navigate(activePage()));
}

async function boot() {
  wireRouter();
  // channels wiring (chat page removed/replaced)
  if (elements.newChannelForm) {
    elements.newChannelForm.addEventListener('submit', createChannel);
  }
  if (elements.addMemberForm) {
    elements.addMemberForm.addEventListener('submit', addMember);
  }
  if (elements.archiveChannelBtn) {
    elements.archiveChannelBtn.addEventListener('click', archiveChannel);
  }
  const channelPostForm = document.getElementById('channelPostForm');
  if (channelPostForm) {
    channelPostForm.addEventListener('submit', postToChannel);
  }
  const newChBtn = document.querySelector('[data-testid="new-channel-button"]');
  if (newChBtn) {
    newChBtn.addEventListener('click', () => {
      location.hash = 'channels';
    });
  }
  navigate(activePage());
  stompConnect();
  try {
    await loadPortalData();
  } catch (error) {
    // channels ready
    console.log('Portal channels UI ready');
  }
}

boot();
