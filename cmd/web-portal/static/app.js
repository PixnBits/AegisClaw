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
  const [dashboard, skills, proposals, monitoring] = await Promise.all([
    fetchJSON('/api/dashboard'),
    fetchJSON('/api/skills'),
    fetchJSON('/api/proposals'),
    fetchJSON('/api/monitoring'),
  ]);

  renderDashboard(dashboard);
  renderSkills(skills);
  renderProposals(proposals);
  renderMonitoring(monitoring);

  // load channels for the new channels page (replaces chat)
  loadChannelsForUI().catch(() => {});
}

let currentChannel = null;

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
  }).catch(() => {
    renderChannelMessages(ch.messages || []);
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
  const from = (document.getElementById('postFrom') && document.getElementById('postFrom').value || 'operator').trim();
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

function sendMessage(event) {
  event.preventDefault();
  const message = elements.messageInput.value.trim();
  if (!message) {
    return;
  }

  elements.messageInput.value = '';
  appendMessage('user', message, { label: 'You', meta: 'local user request' });
  elements.chatStatus.textContent = 'Waiting for first response…';
  startStream(message);
}

function startStream(message) {
  if (state.eventSource) {
    state.eventSource.close();
  }

  state.currentAgentNode = null;
  const url = `/api/chat/stream?message=${encodeURIComponent(message)}&session_id=${encodeURIComponent(state.sessionId)}`;
  state.eventSource = new EventSource(url);
  state.eventSource.onmessage = (event) => {
    const payload = JSON.parse(event.data);
    handleStreamMessage(payload);
  };
  state.eventSource.onerror = () => {
    elements.chatStatus.textContent = 'Stream interrupted. Retry when the portal is ready.';
    if (state.eventSource) {
      state.eventSource.close();
    }
  };
}

function handleStreamMessage(message) {
  if (message.trace_id) {
    state.currentTraceId = message.trace_id;
    elements.currentTraceId.textContent = message.trace_id;
  }

  switch (message.type) {
    case 'user_message':
      break;
    case 'agent_thinking':
      elements.chatStatus.textContent = `${message.content.description} • fast feedback delivered`;
      appendToolEvent('Thinking', message.content.description, message.metadata.timing || 'n/a');
      break;
    case 'tool_call':
      elements.chatStatus.textContent = `Executing ${message.content.tool}`;
      rememberTool(message.content.tool, JSON.stringify(message.content.args));
      appendToolEvent('Tool Call', `${message.content.tool} ${JSON.stringify(message.content.args)}`, message.metadata.timing || 'n/a');
      break;
    case 'tool_result':
      elements.chatStatus.textContent = `${message.content.tool} returned a result`;
      appendToolEvent('Tool Result', `${message.content.tool}: ${message.content.result}`, message.metadata.timing || 'n/a');
      break;
    case 'agent_response':
      elements.chatStatus.textContent = message.content.is_complete ? 'Response complete.' : 'Streaming secure response…';
      updateAgentResponse(message.content.text, Boolean(message.content.is_complete));
      if (message.content.is_complete && state.eventSource) {
        state.eventSource.close();
      }
      break;
    default:
      elements.chatStatus.textContent = 'Received an unexpected stream event.';
  }
}

function appendToolEvent(title, body, timing) {
  const card = document.createElement('article');
  card.className = 'tool-event';
  card.dataset.testid = 'tool-event';
  card.dataset.streamKind = title.toLowerCase().replace(/\s+/g, '-');

  const header = document.createElement('div');
  header.className = 'tool-event__header';
  const strong = document.createElement('strong');
  strong.textContent = title;
  const meta = document.createElement('span');
  meta.className = 'subtle';
  meta.textContent = timing;
  header.append(strong, meta);

  const content = document.createElement('p');
  content.textContent = body;

  card.append(header, content);
  elements.messages.appendChild(card);
  scrollMessages();
}

function appendMessage(kind, text, options = {}) {
  const article = document.createElement('article');
  article.className = `message-bubble ${kind}`;
  article.dataset.kind = kind;
  article.dataset.testid = `${kind}-message`;

  const meta = document.createElement('div');
  meta.className = 'message-meta';
  const label = document.createElement('span');
  label.textContent = options.label || titleCase(kind);
  const trace = document.createElement('span');
  trace.textContent = options.meta || state.currentTraceId;
  meta.append(label, trace);

  const body = document.createElement('div');
  body.className = 'message-markdown';
  body.appendChild(renderSafeMarkdown(text));

  article.append(meta, body);
  elements.messages.appendChild(article);
  scrollMessages();
  return article;
}

function updateAgentResponse(text, isComplete) {
  if (!state.currentAgentNode) {
    state.currentAgentNode = appendMessage('agent', text, { label: 'AegisClaw', meta: state.currentTraceId });
  }

  const body = state.currentAgentNode.querySelector('.message-markdown');
  body.replaceChildren(renderSafeMarkdown(text));
  if (isComplete) {
    state.currentAgentNode = null;
  }
  scrollMessages();
}

// Full streaming Markdown renderer for the primary chat UI (SPA shell).
// Implements the requirements from chat-ui-data-flow.md (incremental agent_response,
// RAIL fast feedback, tables, code fences, bold/italic, links, task lists) while
// preserving the paranoid security model: no third-party libs, escape-first,
// textContent for structure, and only safe innerHTML for already-sanitized inline
// fragments (exact pattern proven in the dedicated /chat renderer).
// Citations: web-portal.md §3 Chat (full client Markdown spec) + Real-time & Streaming;
// chat-ui-data-flow.md (Message Types, UI Rendering Rules, Security & Sanitization,
// Markdown incremental); web-portal.md §Testability & E2E (stable selectors on bubbles).
function renderSafeMarkdown(source) {
  const fragment = document.createDocumentFragment();
  const lines = String(source || '').replace(/\r\n/g, '\n').split('\n');
  let list = null;
  let para = [];

  const commitListToFragment = () => {
    if (list) {
      fragment.appendChild(list);
      list = null;
    }
  };

  const flushPara = () => {
    if (para.length === 0) return;
    const p = document.createElement('p');
    p.innerHTML = renderInlineMarkdownSafe(para.join('<br>'));
    fragment.appendChild(p);
    para = [];
  };

  const renderInlineMarkdownSafe = (input) => {
    let text = String(input || '');
    const codeSpans = [];

    // code spans (protect first)
    text = text.replace(/`([^`]+)`/g, (_, code) => {
      codeSpans.push(code);
      return '@@CODESPAN' + (codeSpans.length - 1) + '@@';
    });

    // links (sanitized)
    text = text.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, url) => {
      const safe = sanitizeURLForMarkdown(url);
      if (!safe) return label + ' (' + url + ')';
      return '<a href="' + escapeForAttr(safe) + '" target="_blank" rel="noopener noreferrer">' + label + '</a>';
    });

    // bold, italic, strike (order matters)
    text = text.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    text = text.replace(/~~([^~]+)~~/g, '<s>$1</s>');
    text = text.replace(/(^|[^*])\*([^*]+)\*(?!\*)/g, '$1<em>$2</em>');

    // restore code spans
    text = text.replace(/@@CODESPAN(\d+)@@/g, (_, idx) => {
      const i = parseInt(idx, 10);
      return '<code>' + codeSpans[i] + '</code>';
    });

    return text;
  };

  const escapeForAttr = (s) => String(s).replace(/"/g, '&quot;').replace(/'/g, '&#39;');

  const sanitizeURLForMarkdown = (raw) => {
    const url = String(raw || '').trim();
    if (!url) return '';
    if (url[0] === '/' || url[0] === '#') return url;
    try {
      const parsed = new URL(url, window.location.origin);
      const p = parsed.protocol.toLowerCase();
      if (p === 'http:' || p === 'https:' || p === 'mailto:') return parsed.href;
      return '';
    } catch (_) {
      return '';
    }
  };

  lines.forEach((line) => {
    const trimmed = line.trim();

    // code fences (```lang\n...\n```) — simple handling safe for incremental re-renders
    if (trimmed.startsWith('```')) {
      flushPara();
      commitListToFragment();
      const pre = document.createElement('pre');
      const code = document.createElement('code');
      code.textContent = trimmed.replace(/^```[a-zA-Z0-9_+-]*\s*/, '');
      pre.appendChild(code);
      fragment.appendChild(pre);
      return;
    }

    if (!trimmed) {
      flushPara();
      commitListToFragment();
      return;
    }

    // task lists and bullet lists
    if (/^-\s+\[([ xX])\]\s+/.test(trimmed) || trimmed.startsWith('- ')) {
      flushPara();
      if (!list) {
        list = document.createElement('ul');
      }
      const li = document.createElement('li');
      const taskMatch = trimmed.match(/^-\s+\[([ xX])\]\s+(.*)$/);
      if (taskMatch) {
        li.innerHTML = '<input type="checkbox" ' + (taskMatch[1].toLowerCase() === 'x' ? 'checked' : '') + ' disabled> ' +
                       renderInlineMarkdownSafe(taskMatch[2]);
      } else {
        li.innerHTML = renderInlineMarkdownSafe(trimmed.slice(2));
      }
      list.appendChild(li);
      return;
    }

    // numbered lists
    if (/^\d+\.\s+/.test(trimmed)) {
      flushPara();
      commitListToFragment();
      const ol = document.createElement('ol');
      const li = document.createElement('li');
      li.innerHTML = renderInlineMarkdownSafe(trimmed.replace(/^\d+\.\s+/, ''));
      ol.appendChild(li);
      fragment.appendChild(ol);
      return;
    }

    flushPara();
    commitListToFragment();

    let node;
    if (trimmed.startsWith('### ')) {
      node = document.createElement('h3');
      node.textContent = trimmed.slice(4);
    } else if (trimmed.startsWith('## ')) {
      node = document.createElement('h2');
      node.textContent = trimmed.slice(3);
    } else if (trimmed.startsWith('# ')) {
      node = document.createElement('h1');
      node.textContent = trimmed.slice(2);
    } else if (trimmed.startsWith('> ')) {
      node = document.createElement('blockquote');
      node.textContent = trimmed.slice(2);
    } else {
      // accumulate paragraph lines for inline markdown
      para.push(trimmed);
      return;
    }
    fragment.appendChild(node);
  });

  flushPara();
  commitListToFragment();

  const wrapper = document.createElement('div');
  wrapper.className = 'message-markdown';
  wrapper.appendChild(fragment);
  return wrapper;
}

function rememberTool(tool, detail) {
  state.recentTools.unshift({ tool, detail });
  state.recentTools = state.recentTools.slice(0, 5);
  elements.recentToolsList.replaceChildren(...state.recentTools.map((entry) => buildListCard(entry.tool, entry.detail)));
}

function scrollMessages() {
  elements.messages.scrollTop = elements.messages.scrollHeight;
}

function titleCase(value) {
  return String(value)
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (match) => match.toUpperCase());
}

function activePage() {
  // Strip any query params from the hash before looking up the page (e.g. #chat?id=1 → chat).
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
  navigate(activePage());
  try {
    await loadPortalData();
  } catch (error) {
    // no chatStatus anymore; could log
    console.warn('Portal data unavailable');
  }
}

boot();
