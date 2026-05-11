const state = {
  sessionId: `sess_${Date.now()}`,
  eventSource: null,
  currentAgentNode: null,
  currentTraceId: 'waiting',
  recentTools: [],
};

const PAGE_TITLES = {
  dashboard: 'Dashboard',
  chat: 'Conversations',
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
  chatStatus: document.getElementById('chatStatus'),
  messages: document.getElementById('messages'),
  chatForm: document.getElementById('chatForm'),
  messageInput: document.getElementById('messageInput'),
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

// Intentionally supports only headings, bullet lists, and paragraphs.
// The portal avoids third-party markdown libraries here so streamed content
// stays self-contained and is rendered only through textContent-based nodes.
function renderSafeMarkdown(source) {
  const fragment = document.createDocumentFragment();
  const lines = source.split(/\n+/);
  let list = null;

  const commitListToFragment = () => {
    if (list) {
      fragment.appendChild(list);
      list = null;
    }
  };

  lines.forEach((line) => {
    const trimmed = line.trim();
    if (!trimmed) {
      commitListToFragment();
      return;
    }

    if (trimmed.startsWith('- ')) {
      if (!list) {
        list = document.createElement('ul');
      }
      const item = document.createElement('li');
      item.textContent = trimmed.slice(2);
      list.appendChild(item);
      return;
    }

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
    } else {
      node = document.createElement('p');
      node.textContent = trimmed;
    }
    fragment.appendChild(node);
  });

  commitListToFragment();
  const wrapper = document.createElement('div');
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
  const hash = location.hash.slice(1);
  return Object.prototype.hasOwnProperty.call(PAGE_TITLES, hash) ? hash : 'dashboard';
}

function navigate(page) {
  // Validate against the known-page whitelist; default to dashboard for unknown values.
  const safePage = Object.prototype.hasOwnProperty.call(PAGE_TITLES, page) ? page : 'dashboard';
  document.querySelectorAll('[data-page]').forEach((panel) => {
    panel.hidden = panel.dataset.page !== safePage;
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
  elements.chatForm.addEventListener('submit', sendMessage);
  navigate(activePage());
  try {
    await loadPortalData();
  } catch (error) {
    elements.chatStatus.textContent = 'Portal data is temporarily unavailable.';
  }
}

boot();
