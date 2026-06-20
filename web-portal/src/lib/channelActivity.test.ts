import { describe, expect, it } from 'vitest';
import { appendFeedItem, feedItemFromActivityPayload } from './channelActivity';
import { FeedItem } from '@/contracts';

describe('channelActivity', () => {
  it('parses STOMP channel.activity message events', () => {
    const item = feedItemFromActivityPayload(
      {
        type: 'channel.activity',
        channel_id: 'main',
        event: JSON.stringify({
          kind: 'message',
          from: 'project-manager-main',
          content: 'Hello **world**',
          ts: '2026-06-18T12:00:00Z',
        }),
      },
      'main',
    );
    expect(item).not.toBeNull();
    expect(item?.from).toBe('project-manager-main');
    expect(item?.content).toContain('Hello');
  });

  it('dedupes appended feed items by from/content/ts', () => {
    const base: FeedItem = {
      id: 'a',
      kind: 'agent_update',
      from: 'pm',
      content: 'hi',
      ts: '1',
      channelId: 'main',
    };
    const dup: FeedItem = { ...base, id: 'b' };
    const next = appendFeedItem([base], dup);
    expect(next).toHaveLength(1);
  });
});
