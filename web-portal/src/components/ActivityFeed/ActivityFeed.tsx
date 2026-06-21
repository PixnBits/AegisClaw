import { FeedItem as FeedItemType } from '@/contracts';
import { VirtualList } from '@/components/ui/VirtualList';
import { FeedItemRow } from './FeedItem';
import './ActivityFeed.css';

type Props = {
  items: FeedItemType[];
  channelId: string;
  onCollapseAll?: () => void;
  onExpandRecent?: () => void;
};

export function ActivityFeed({ items, channelId, onCollapseAll, onExpandRecent }: Props) {
  const isEmpty = items.length === 0;

  return (
    <section className="activity-feed" aria-label="Channel activity feed">
      {(onCollapseAll || onExpandRecent) && (
        <div className="activity-feed__controls" data-testid="feed-controls">
          {onCollapseAll && (
            <button type="button" className="link-button activity-feed__control" data-testid="collapse-all-reasoning" onClick={onCollapseAll}>
              Collapse all reasoning
            </button>
          )}
          {onExpandRecent && (
            <button type="button" className="link-button activity-feed__control" data-testid="expand-recent-reasoning" onClick={onExpandRecent}>
              Expand recent
            </button>
          )}
        </div>
      )}
      <div
        className={`chat-stream${isEmpty ? ' chat-stream--empty' : ''}`}
        data-testid="channel-messages"
        data-empty={isEmpty ? 'true' : undefined}
        aria-live="polite"
        role={isEmpty ? undefined : 'log'}
        tabIndex={isEmpty ? undefined : 0}
        aria-label={isEmpty ? undefined : 'Channel messages'}
      >
        {isEmpty ? (
          <div className="activity-feed__empty" data-testid="feed-empty-state">
            <p className="activity-feed__empty-title">Quiet for now</p>
            <p className="activity-feed__empty-desc">
              Give the PM a goal and activity will appear here — agent updates, tool calls, and Court decisions.
            </p>
          </div>
        ) : (
          <VirtualList
            items={items}
            getKey={(item) => item.id}
            testId="channel-messages-virtual"
            ariaLabel="Channel messages"
            className="activity-feed__virtual"
            renderItem={(item) => <FeedItemRow item={item} channelId={channelId} />}
          />
        )}
      </div>
    </section>
  );
}