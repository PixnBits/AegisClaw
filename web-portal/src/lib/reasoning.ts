import { FeedItem, FeedItemKind, ReasoningPolicy } from '@/contracts';

/** Whether reasoning body should render expanded per policy + item state. */
export function shouldExpandReasoning(
  item: FeedItem,
  policy: ReasoningPolicy,
  userExpanded?: boolean,
): boolean {
  if (item.kind === 'human_message' || item.kind === 'court_decision' || item.kind === 'system_error') return true;
  if (userExpanded) return true;
  if (item.kind === 'channel_status') {
    // Latest status (decisive=true from prepare) shown full by default; older (decisive=false) collapsed.
    return !!item.decisive;
  }

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
    if (item.kind !== 'agent_reasoning' && item.kind !== 'agent_update' && item.kind !== 'tool_call' && item.kind !== 'channel_status') {
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
  let kind: FeedItemKind = isCourt && content.includes('approved')
    ? 'court_decision'
    : isAgent
      ? 'agent_update'
      : 'human_message';
  if (from === 'system') {
    const lower = content.toLowerCase();
    if (lower.startsWith('status:') || lower.includes('turns delivered') || lower.includes('scheduled turns delivered') || lower.includes('all scheduled turns delivered')) {
      kind = 'channel_status';
    } else if (lower.includes('[turn error]') || lower.includes('delivery to') && lower.includes('failed')) {
      kind = 'system_error';
    }
  }

  return {
    id: `msg-${channelId}-${index}-${msg.ts ?? Date.now()}`,
    kind,
    from,
    content,
    ts: String(msg.ts ?? new Date().toISOString()),
    channelId,
    inFlight: false,
    decisive: kind === 'human_message' || kind === 'court_decision' || kind === 'system_error',
  };
}

/** Post-process feed so only the most recent channel_status is shown full by default;
 * older status entries get collapsedSummary so UI can collapse them for low noise.
 */
export function prepareChannelStatusFeed(items: FeedItem[]): FeedItem[] {
  const statusIndices: number[] = [];
  for (let i = 0; i < items.length; i++) {
    if (items[i].kind === 'channel_status') {
      statusIndices.push(i);
    }
  }
  if (statusIndices.length <= 1) return items;

  const latest = statusIndices[statusIndices.length - 1];
  return items.map((item, idx) => {
    if (item.kind !== 'channel_status') return item;
    if (idx === latest) {
      return { ...item, decisive: true, collapsedSummary: undefined };
    }
    const short = item.content.length > 60 ? item.content.substring(0, 57) + '...' : item.content;
    return {
      ...item,
      collapsedSummary: short,
      decisive: false,
    };
  });
}