import { FeedItem as FeedItemType } from '@/contracts';
import { shouldExpandReasoning, buildCollapsedSummary } from '@/lib/reasoning';
import { usePolicyStore } from '@/store/policyStore';
import { usePortalStore } from '@/store/portalStore';
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
  const isReasoning =
    item.kind === 'agent_reasoning' || item.kind === 'tool_call' || item.kind === 'agent_update';

  return (
    <article
      className={`feed-item feed-item--${item.kind}${item.inFlight ? ' feed-item--live' : ''}`}
      data-testid={`feed-item-${item.id}`}
    >
      <header className="feed-item__header">
        <strong>{item.from}</strong>
        {item.inFlight && <span className="feed-item__live-badge">Live</span>}
        <time className="subtle">{formatTime(item.ts)}</time>
      </header>

      {isReasoning && !expand ? (
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
            <div className="feed-item__content">{item.content}</div>
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