import { FeedItem as FeedItemType } from '@/contracts';
import { formatPersonaLabel } from '@/lib/display';
import { shouldExpandReasoning, buildCollapsedSummary } from '@/lib/reasoning';
import { usePolicyStore } from '@/store/policyStore';
import { usePortalStore } from '@/store/portalStore';
import { MarkdownContent } from '@/components/ui/MarkdownContent';
import './ActivityFeed.css';

type Props = {
  item: FeedItemType;
  channelId: string;
};

export function FeedItemRow({ item, channelId }: Props) {
  const policy = usePolicyStore((s) => s.effectivePolicy(channelId));
  const expanded = usePortalStore((s) => s.expandedReasoning.has(item.id));
  const toggleExpanded = usePortalStore((s) => s.toggleReasoningExpanded);
  const expand = shouldExpandReasoning(item, policy, expanded);
  const isChannelStatus = item.kind === 'channel_status';
  const isSystemError = item.kind === 'system_error';
  const isReasoning =
    !isChannelStatus && !isSystemError && (item.kind === 'agent_reasoning' || item.kind === 'tool_call' || item.kind === 'agent_update');

  return (
    <article
      className={`feed-item feed-item--${item.kind}${item.inFlight ? ' feed-item--live' : ''}${isChannelStatus ? ' feed-item--status' : ''}${isSystemError ? ' feed-item--error' : ''}`}
      data-testid={`feed-item-${item.id}`}
    >
      <header className="feed-item__header">
        <strong>{formatPersonaLabel(item.from)}</strong>
        {item.inFlight && <span className="feed-item__live-badge">Live</span>}
        <time className="subtle">{formatTime(item.ts)}</time>
      </header>

      {isChannelStatus && !expand ? (
        <div className="feed-item__collapsed feed-item__status-collapsed">
          <span>📍 {item.collapsedSummary || item.content}</span>
          <button
            type="button"
            className="feed-item__show-reasoning"
            data-testid={`show-status-${item.id}`}
            onClick={() => toggleExpanded(item.id)}
          >
            Show full status
          </button>
        </div>
      ) : isChannelStatus ? (
        <div className="feed-item__content feed-item__status">
          <span>📍 {item.content}</span>
          {expanded && (
            <button
              type="button"
              className="feed-item__show-reasoning"
              onClick={() => toggleExpanded(item.id)}
            >
              Hide details
            </button>
          )}
        </div>
      ) : isSystemError ? (
        <div className="feed-item__content feed-item__error">
          ⚠️ {item.content}
        </div>
      ) : isReasoning && !expand ? (
        <div className="feed-item__collapsed">
          <span>{item.collapsedSummary || buildCollapsedSummary(item)}</span>
          <button
            type="button"
            className="feed-item__show-reasoning"
            data-testid={`show-reasoning-${item.id}`}
            onClick={() => toggleExpanded(item.id)}
          >
            Show reasoning
          </button>
        </div>
      ) : (
        <>
          {item.reasoningSteps?.length ? (
            <div className="feed-item__reasoning" data-testid="reasoning-steps">
              {item.reasoningSteps.map((step, i) => (
                <div
                  key={i}
                  className={`reasoning-step reasoning-step--${step.phase.toLowerCase()}${step.status === 'live' ? ' reasoning-step--live' : ''}`}
                  data-testid={`reasoning-step-${step.phase}`}
                >
                  <span className="reasoning-step__phase">{step.phase}</span>
                  <span>{step.content}</span>
                  {step.tool && <code className="reasoning-step__tool">{step.tool}</code>}
                </div>
              ))}
            </div>
          ) : (
            <div className="feed-item__content">
              {item.kind === 'human_message' || item.kind === 'agent_update' || item.kind === 'court_decision' ? (
                <MarkdownContent
                  content={item.content}
                  context={item.kind === 'court_decision' ? 'proposal' : 'chat'}
                />
              ) : item.kind === 'tool_call' ? (
                <MarkdownContent content={item.content} context="trace" />
              ) : (
                item.content
              )}
            </div>
          )}
          {isReasoning && item.decisive && expanded && (
            <button
              type="button"
              className="feed-item__show-reasoning"
              onClick={() => toggleExpanded(item.id)}
            >
              Hide reasoning
            </button>
          )}
        </>
      )}
    </article>
  );
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString();
}