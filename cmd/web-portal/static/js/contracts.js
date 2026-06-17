// STOMP topic helpers — mirrors internal/dashboard/contracts (real-time-contracts.md).

export const TOPIC = {
  overviewStats: '/topic/overview.stats',
  canvasEvents: '/topic/canvas.events',
  approvalsPending: '/topic/approvals.pending',
  channelActivity: (channelId) => `/topic/channel.${channelId}.activity`,
  legacyChannelMessages: (channelId) => `/topic/channels.${channelId}.messages`,
  harnessUpdates: (planId) => `/topic/harness.${planId}.updates`,
  conversationUpdates: (sessionId) => `/topic/conversation.${sessionId}.updates`,
  proposalUpdates: (proposalId) => `/topic/proposal.${proposalId}.updates`,
};

export const PIPELINE_STAGES = [
  'Plan',
  'Delegate',
  'Execute',
  'Propose',
  'Court Review',
  'Apply',
];

export const EVENT = {
  overviewStats: 'overview.stats',
  channelActivity: 'channel.activity',
  harnessPlanCreated: 'harness.plan.created',
  harnessTaskAssigned: 'harness.task.assigned',
  harnessTaskProgress: 'harness.task.progress',
  harnessStageTransition: 'harness.stage.transition',
  harnessProposalCreated: 'harness.proposal.created',
  canvasEvent: 'canvas.event',
};

export function topicsForView(view, ctx = {}) {
  const topics = [];
  switch (view) {
    case 'home':
      topics.push(TOPIC.overviewStats, TOPIC.approvalsPending);
      break;
    case 'dashboard':
      topics.push(TOPIC.overviewStats, TOPIC.canvasEvents, TOPIC.approvalsPending);
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
      topics.push(TOPIC.canvasEvents);
      if (ctx.planId) topics.push(TOPIC.harnessUpdates(ctx.planId));
      break;
    case 'trace':
      if (ctx.sessionId) topics.push(TOPIC.conversationUpdates(ctx.sessionId));
      break;
    default:
      break;
  }
  return topics;
}