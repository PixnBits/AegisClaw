import { FeedItem as FeedItemType } from '@/contracts';
import { FeedItemRow } from './FeedItem';
import './ActivityFeed.css';

type Props = {
  items: FeedItemType[];
  channelId: string;
  onCollapseAll?: () => void;
  onExpandRecent?: () => void;
};

export function ActivityFeed({ items, channelId, onCollapseAll, onExpandRecent }: Props) {
  return (
    <section className="activity-feed" aria-label="Channel activity feed">
      {(onCollapseAll || onExpandRecent) && (
        <div className="activity-feed__controls" data-testid="feed-controls">
          {onCollapseAll && (
            <button type="button" className="secondary-button" data-testid="collapse-all-reasoning" onClick={onCollapseAll}>
              Collapse all reasoning
            </button>
          )}
          {onExpandRecent && (
            <button type="button" className="secondary-button" data-testid="expand-recent-reasoning" onClick={onExpandRecent}>
              Expand recent
            </button>
          )}
        </div>
      )}
      <div
        className="chat-stream"
        data-testid="channel-messages"
        data-empty={items.length === 0 ? 'true' : undefined}
        aria-live="polite"
      >
        {items.length === 0 ? (
          <p className="subtle activity-feed__empty">No messages yet. Give the PM a goal to get started.</p>
        ) : (
          items.map((item) => <FeedItemRow key={item.id} item={item} channelId={channelId} />)
        )}
      </div>
    </section>
  );
}