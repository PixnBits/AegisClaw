// Mirrors internal/dashboard/contracts — single source for portal data shapes.

export const PIPELINE_STAGES = [
  'Plan',
  'Delegate',
  'Execute',
  'Propose',
  'Court Review',
  'Apply',
] as const;

export type PipelineStageName = (typeof PIPELINE_STAGES)[number];

export const EVENT = {
  overviewStats: 'overview.stats',
  monitoringStats: 'monitoring.stats',
  channelActivity: 'channel.activity',
  conversationUpdate: 'conversation.update',
  canvasEvent: 'canvas.event',
  harnessPlanCreated: 'harness.plan.created',
  harnessTaskAssigned: 'harness.task.assigned',
  harnessTaskProgress: 'harness.task.progress',
  harnessStageTransition: 'harness.stage.transition',
  harnessProposalCreated: 'harness.proposal.created',
} as const;

export const TOPIC = {
  overviewStats: '/topic/overview.stats',
  monitoringStats: '/topic/monitoring.stats',
  canvasEvents: '/topic/canvas.events',
  approvalsPending: '/topic/approvals.pending',
  channelActivity: (channelId: string) => `/topic/channel.${channelId}.activity`,
  harnessUpdates: (planId: string) => `/topic/harness.${planId}.updates`,
  conversationUpdates: (sessionId: string) => `/topic/conversation.${sessionId}.updates`,
  proposalUpdates: (proposalId: string) => `/topic/proposal.${proposalId}.updates`,
} as const;

export type PortalView =
  | 'home'
  | 'channels'
  | 'dashboard'
  | 'court'
  | 'canvas'
  | 'agents'
  | 'skills'
  | 'audit'
  | 'settings'
  | 'trace'
  | 'monitoring'
  | 'teams';

export type ViewContext = {
  channelId?: string;
  /** All known channel IDs — subscribe for live updates + unread badges */
  channelIds?: string[];
  planId?: string;
  planIds?: string[];
  sessionId?: string;
  proposalId?: string;
};

export function topicsForView(view: PortalView, ctx: ViewContext = {}): string[] {
  const topics: string[] = [];
  const channelTopics = new Set<string>();
  if (ctx.channelIds?.length) {
    for (const id of ctx.channelIds) {
      channelTopics.add(TOPIC.channelActivity(id));
    }
  } else if (ctx.channelId) {
    channelTopics.add(TOPIC.channelActivity(ctx.channelId));
  }
  switch (view) {
    case 'home':
      topics.push(TOPIC.overviewStats, TOPIC.approvalsPending);
      break;
    case 'dashboard':
    case 'monitoring':
      topics.push(TOPIC.overviewStats, TOPIC.monitoringStats, TOPIC.canvasEvents, TOPIC.approvalsPending);
      break;
    case 'channels':
      if (ctx.channelId) {
        topics.push(TOPIC.channelActivity(ctx.channelId));
        if (ctx.planId) topics.push(TOPIC.harnessUpdates(ctx.planId));
      }
      break;
    case 'court':
      topics.push(TOPIC.approvalsPending);
      if (ctx.proposalId) topics.push(TOPIC.proposalUpdates(ctx.proposalId));
      break;
    case 'canvas':
      topics.push(TOPIC.canvasEvents, TOPIC.monitoringStats);
      if (ctx.planIds?.length) {
        for (const id of ctx.planIds) topics.push(TOPIC.harnessUpdates(id));
      } else if (ctx.planId) topics.push(TOPIC.harnessUpdates(ctx.planId));
      break;
    case 'trace':
      if (ctx.sessionId) topics.push(TOPIC.conversationUpdates(ctx.sessionId));
      break;
    default:
      break;
  }
  for (const topic of channelTopics) {
    if (!topics.includes(topic)) topics.push(topic);
  }
  return topics;
}

export type MonitoringStats = {
  type: typeof EVENT.monitoringStats;
  timestamp: string;
  stats?: {
    running_vms?: number;
    background_tasks?: number;
    cpu_usage?: string;
    memory_usage?: string;
  };
  agents?: unknown[];
};

export type SecurityIndicator = {
  id: string;
  label: string;
  status: 'ok' | 'warn' | 'error' | string;
  detail?: string;
};

export type SecurityPosture = {
  indicators: SecurityIndicator[];
  store_collab_ready?: boolean;
  court_personas_online?: number;
  web_portal_status?: string;
  collab?: string;
  updated_at?: string;
};

export type StageStatus = {
  name: PipelineStageName | string;
  status: 'pending' | 'in_progress' | 'completed' | 'failed';
};

export type Plan = {
  plan_id: string;
  channel_id: string;
  goal: string;
  created_at?: string;
  status: 'active' | 'completed' | 'cancelled';
  stages: StageStatus[];
};

export type NarrowTask = {
  task_id: string;
  plan_id: string;
  agent_persona: string;
  scope: string;
  status: string;
  current_stage: string;
  progress: number;
  last_update?: string;
  summary?: string;
};

export type HarnessState = {
  plan?: Plan | null;
  tasks: NarrowTask[];
};

export type ChannelMessage = {
  from: string;
  content: string;
  ts?: string | number;
};

export type Channel = {
  id: string;
  members?: Array<{ role?: string; agent_id?: string }>;
  messages?: ChannelMessage[];
  archived?: boolean;
};

export type FeedItemKind =
  | 'human_message'
  | 'agent_reasoning'
  | 'agent_update'
  | 'tool_call'
  | 'court_decision'
  | 'proposal_event'
  | 'handoff'
  | 'channel_status'
  | 'system_error';

export type ReasoningPhase = 'Observe' | 'Think' | 'Plan' | 'Act' | 'Judge';

export type FeedItem = {
  id: string;
  kind: FeedItemKind;
  from: string;
  content: string;
  ts: string;
  channelId: string;
  /** Live reasoning steps (Observe → Think → Plan → Act) */
  reasoningSteps?: Array<{
    phase: ReasoningPhase;
    content: string;
    tool?: string;
    status?: 'live' | 'completed';
  }>;
  /** Set when a decisive result occurs — triggers collapse under Progressive policy */
  decisive?: boolean;
  collapsedSummary?: string;
  proposalId?: string;
  inFlight?: boolean;
};

export type ReasoningPolicy = 'progressive' | 'paranoid' | 'velocity';

export type OverviewStats = {
  type: typeof EVENT.overviewStats;
  timestamp: string;
  active_agents: { total: number; by_role?: Record<string, number> };
  background_tasks: { total: number; avg_progress: number };
  pending_proposals: number;
  token_usage?: { total?: number; channel?: number };
};

export type ActiveWorkItem = {
  id: string;
  persona: string;
  scope: string;
  stage: string;
  progress: string | number;
  status: string;
  channel_id: string;
  proposal_id?: string;
  last_update?: string;
};

export type DashboardData = {
  notifications: number;
  safe_mode: boolean;
  channel_count: number;
  quick_stats: {
    active_agents: number;
    background_tasks: number;
    skills_installed: number;
    pending_proposals: number;
    channel_count?: number;
  };
  agents: Array<{ name: string; status: string; task: string; progress: string }>;
  active_work: ActiveWorkItem[];
  recent_activity: unknown[];
};

// LLM usage aggregates (Phase 1)
export type LLMUsageSummary = {
  grand?: { calls?: number; tokens_prompt?: number; tokens_completion?: number; tokens_total?: number; by_model?: Record<string, number> };
  last_hour?: { calls?: number; tokens_prompt?: number; tokens_completion?: number };
  today?: { calls?: number; tokens_prompt?: number; tokens_completion?: number };
  mtd?: { calls?: number; tokens_prompt?: number; tokens_completion?: number };
  by_agent?: Record<string, any>;
  records?: any[]; // for recent time-series bucketing
  models?: Record<string, number>;
  record_count?: number;
};

export type Proposal = {
  id: string;
  title: string;
  status: string;
  summary?: string;
  votes?: string;
};

export type AgentTrace = {
  agent_id: string;
  session_id: string;
  phases: Array<{
    phase: ReasoningPhase | string;
    summary?: string;
    tool?: string;
    status?: string;
    ts?: string;
  }>;
};