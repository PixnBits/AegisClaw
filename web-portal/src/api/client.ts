import type {
  AgentTrace,
  Channel,
  DashboardData,
  HarnessState,
  Proposal,
} from '@/contracts';
import { sanitizeProposalNote } from '@/lib/sanitize';

/** Court proposal actions exposed by the portal bridge (see internal/dashboard/contracts/bridge.go). */
export type ProposalAction = 'approve' | 'reject' | 'defer';

const PROPOSAL_ACTIONS = new Set<ProposalAction>(['approve', 'reject', 'defer']);

function assertProposalAction(action: string): ProposalAction {
  if (!PROPOSAL_ACTIONS.has(action as ProposalAction)) {
    throw new Error(`Invalid proposal action: ${action}`);
  }
  return action as ProposalAction;
}

async function fetchJSON<T>(url: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(url, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(options.headers as Record<string, string>) },
    ...options,
  });
  if (!response.ok) throw new Error(`Request failed: ${url} (${response.status})`);
  return response.json() as Promise<T>;
}

export const api = {
  dashboard: () => fetchJSON<DashboardData>('/api/dashboard'),
  monitoring: () => fetchJSON<{ stats: Record<string, unknown>; agents: unknown[] }>('/api/monitoring'),
  securityPosture: () => fetchJSON<import('@/contracts').SecurityPosture>('/api/security/posture'),
  channels: () => fetchJSON<{ channels: Channel[] }>('/api/channels'),
  channel: (id: string) => fetchJSON<Channel>(`/api/channels/${id}`),
  createChannel: (id: string) =>
    fetchJSON<{ id: string }>('/api/channels', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    }),
  postChannel: (id: string, content: string, from = 'user') =>
    fetchJSON<{ ok: boolean }>(`/api/channels/${id}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from, content }),
    }),
  harness: (channelId: string) => fetchJSON<HarnessState>(`/api/channels/${channelId}/harness`),
  goals: (goal: string, channelId = 'main') =>
    fetchJSON<{
      plan_id: string;
      channel_id: string;
      goal: string;
      stages: HarnessState['plan'] extends infer P ? P extends { stages: infer S } ? S : never : never;
      preview: boolean;
    }>('/api/goals', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ goal, channel_id: channelId }),
    }),
  skills: () => fetchJSON<unknown[]>('/api/skills'),
  proposals: () => fetchJSON<Proposal[]>('/api/proposals'),
  proposalReviews: (id: string) => fetchJSON<{ reviews: unknown[] }>(`/api/proposals/${id}/reviews`),
  proposalAction: (id: string, action: ProposalAction, note?: string) => {
    const safeAction = assertProposalAction(action);
    const safeId = encodeURIComponent(id);
    const safeNote = sanitizeProposalNote(note);
    const body: { note?: string } = {};
    if (safeNote !== undefined) body.note = safeNote;
    return fetchJSON<{ ok: boolean }>(`/api/proposals/${safeId}/${safeAction}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Aegis-Confirmed': '1' },
      body: JSON.stringify(body),
    });
  },
  exportProposal: (id: string, format = 'report') =>
    fetch(`/api/proposals/${encodeURIComponent(id)}/export?format=${encodeURIComponent(format)}`, {
      credentials: 'same-origin',
    }),
  addMember: (channelId: string, role: string) =>
    fetchJSON<{ ok: boolean }>(`/api/channels/${channelId}/members`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role }),
    }),
  removeMember: (channelId: string, role: string) =>
    fetchJSON<{ ok: boolean }>(`/api/channels/${channelId}/members/remove`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Aegis-Confirmed': '1' },
      body: JSON.stringify({ role }),
    }),
  archiveChannel: (channelId: string) =>
    fetchJSON<{ ok: boolean }>(`/api/channels/${channelId}/archive`, {
      method: 'POST',
      headers: { 'X-Aegis-Confirmed': '1' },
    }),
  canvas: (channelId: string) => fetchJSON<Record<string, unknown>>(`/api/canvas?channel_id=${encodeURIComponent(channelId)}`),
  agents: () => fetchJSON<{ agents: Array<{ name: string; status: string; task: string; progress: string; last_seen_seq?: number; cycles_since_turn?: number; last_outcome?: string; pending?: boolean; last_activity?: string; channel?: string }> }>('/api/agents'),
  agentTrace: (agentId: string) => fetchJSON<AgentTrace>(`/api/agents/${encodeURIComponent(agentId)}/trace`),
  agentAction: (agentId: string, action: 'pause' | 'resume' | 'cancel') =>
    fetchJSON<{ ok: boolean }>(`/api/agents/${encodeURIComponent(agentId)}/${action}`, {
      method: 'POST',
      headers: { 'X-Aegis-Confirmed': '1' },
    }),
  agentPermissions: (agentId: string) =>
    fetchJSON<{
      agent_id: string;
      grants: unknown[];
      requests: unknown[];
      visibility: unknown[];
      snapshot: unknown;
    }>(`/api/agents/${encodeURIComponent(agentId)}/permissions`),
  agentPermissionAction: (agentId: string, action: 'grant' | 'revoke' | 'hide', capability: string, reason?: string) =>
    fetchJSON<{ ok: boolean }>(`/api/agents/${encodeURIComponent(agentId)}/permissions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Aegis-Confirmed': '1' },
      body: JSON.stringify({ action, capability, reason }),
    }),
  cisoDelegation: () => fetchJSON<{ enabled: boolean }>('/api/settings/ciso-delegation'),
  setCisoDelegation: (enabled: boolean) =>
    fetchJSON<{ ok: boolean }>('/api/settings/ciso-delegation', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Aegis-Confirmed': '1' },
      body: JSON.stringify({ enabled }),
    }),
  activeWork: () => fetchJSON<{ items: unknown[]; count: number }>('/api/active-work'),
  llmUsage: () => fetchJSON<Record<string, unknown>>('/api/llm-usage'),
  agentSettings: (id: string) => fetchJSON<{ agent: string; settings?: unknown; soul?: string }>(`/api/agents/${encodeURIComponent(id)}/settings`),
  saveAgentSettings: (id: string, body: { settings?: unknown; soul?: string }) =>
    fetchJSON<{ ok: boolean }>(`/api/agents/${encodeURIComponent(id)}/settings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Aegis-Confirmed': '1' },
      body: JSON.stringify(body),
    }),
};