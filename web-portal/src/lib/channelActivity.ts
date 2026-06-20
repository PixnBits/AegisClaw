import { FeedItem } from '@/contracts';
import { messageToFeedItem } from '@/lib/reasoning';

/** Parse STOMP channel.activity payload into a feed item when possible */
export function feedItemFromActivityPayload(
  payload: Record<string, unknown>,
  channelId: string,
): FeedItem | null {
  const type = String(payload.type || '');
  if (type !== 'channel.activity') return null;

  const chId = String(payload.channel_id || channelId);
  let inner: Record<string, unknown> | null = null;

  const event = payload.event;
  if (typeof event === 'string') {
    try {
      inner = JSON.parse(event) as Record<string, unknown>;
    } catch {
      return null;
    }
  } else if (event && typeof event === 'object') {
    inner = event as Record<string, unknown>;
  }

  if (!inner) return null;

  if (inner.type && String(inner.type).startsWith('harness.')) {
    return null;
  }

  const kind = String(inner.kind || '');
  if (kind === 'message' || inner.from || inner.content) {
    const from = String(inner.from || 'unknown');
    const content = String(inner.content || '');
    const ts = String(inner.ts || payload.timestamp || new Date().toISOString());
    return messageToFeedItem({ from, content, ts }, chId, Date.now());
  }

  return null;
}

export function appendFeedItem(items: FeedItem[], item: FeedItem): FeedItem[] {
  const key = `${item.from}|${item.content}|${item.ts}`;
  if (items.some((i) => `${i.from}|${i.content}|${i.ts}` === key)) return items;
  return [...items, item];
}
