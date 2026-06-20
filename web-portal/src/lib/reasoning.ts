import { FeedItem, ReasoningPolicy } from '@/contracts';

/** Whether reasoning body should render expanded per policy + item state. */
export function shouldExpandReasoning(
  item: FeedItem,
  policy: ReasoningPolicy,
  userExpanded?: boolean,
): boolean {
  if (item.kind === 'human_message' || item.kind === 'court_decision') return true;
  if (userExpanded) return true;

  switch (policy) {
    case 'paranoid':
      return true;
    case 'velocity':
      return Boolean(item.inFlight);
    case 'progressive':
    default:
      if (item.inFlight) return true;
      if (item.decisive) return false;
      return !item.collapsedSummary;
  }
}

/** One-line summary after collapse (Progressive default). */
export function buildCollapsedSummary(item: FeedItem): string {
  if (item.collapsedSummary) return item.collapsedSummary;
  const steps = item.reasoningSteps?.length ?? 0;
  const toolCalls = item.reasoningSteps?.filter((s) => s.phase === 'Act').length ?? 0;
  const persona = item.from || 'Agent';
  if (item.kind === 'proposal_event') {
    return `${persona} submitted proposal — ${steps} reasoning steps`;
  }
  return `${persona} completed scope with ${toolCalls || steps} tool call${(toolCalls || steps) === 1 ? '' : 's'}`;
}

/** Mark in-flight reasoning as decisive (collapse under Progressive). */
export function finalizeReasoningItem(item: FeedItem): FeedItem {
  return {
    ...item,
    inFlight: false,
    decisive: true,
    collapsedSummary: buildCollapsedSummary({ ...item, inFlight: false }),
    reasoningSteps: item.reasoningSteps?.map((s) => ({ ...s, status: 'completed' as const })),
  };
}

/** Feed-level collapse all reasoning (mobile density control). */
export function collapseAllReasoning(items: FeedItem[]): FeedItem[] {
  return items.map((item) => {
    if (item.kind !== 'agent_reasoning' && item.kind !== 'agent_update' && item.kind !== 'tool_call') {
      return item;
    }
    return finalizeReasoningItem(item);
  });
}

/** Expand recent N reasoning items. */
export function expandRecentReasoning(items: FeedItem[], count = 3): Set<string> {
  const ids = new Set<string>();
  let seen = 0;
  for (let i = items.length - 1; i >= 0 && seen < count; i--) {
    const item = items[i];
    if (item.kind === 'agent_reasoning' || item.kind === 'tool_call') {
      ids.add(item.id);
      seen++;
    }
  }
  return ids;
}

export function messageToFeedItem(
  msg: { from?: string; content?: string; ts?: string | number },
  channelId: string,
  index: number,
): FeedItem {
  const from = msg.from || 'unknown';
  const content = typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content ?? '');
  const isAgent = from !== 'user' && !from.startsWith('user:');
  const isCourt = from.includes('court') || content.toLowerCase().includes('proposal');
  const kind = isCourt && content.includes('approved')
    ? 'court_decision'
    : isAgent
      ? 'agent_update'
      : 'human_message';

  return {
    id: `msg-${channelId}-${index}-${msg.ts ?? Date.now()}`,
    kind,
    from,
    content,
    ts: String(msg.ts ?? new Date().toISOString()),
    channelId,
    inFlight: false,
    decisive: kind === 'human_message' || kind === 'court_decision',
  };
}