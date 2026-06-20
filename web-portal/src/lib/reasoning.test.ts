import { describe, expect, it } from 'vitest';
import {
  shouldExpandReasoning,
  buildCollapsedSummary,
  finalizeReasoningItem,
  collapseAllReasoning,
} from './reasoning';
import { FeedItem } from '@/contracts';

const reasoningItem: FeedItem = {
  id: 'r1',
  kind: 'agent_reasoning',
  from: 'researcher',
  content: 'Analyzing Zig security model…',
  ts: '2026-06-17T12:00:00Z',
  channelId: 'main',
  inFlight: true,
  reasoningSteps: [
    { phase: 'Observe', content: 'Reading docs', status: 'live' },
    { phase: 'Act', content: 'web_search', tool: 'web_search', status: 'live' },
  ],
};

describe('reasoning visibility', () => {
  it('expands live reasoning under Progressive policy', () => {
    expect(shouldExpandReasoning(reasoningItem, 'progressive')).toBe(true);
  });

  it('collapses completed reasoning under Progressive policy', () => {
    const done = finalizeReasoningItem(reasoningItem);
    expect(shouldExpandReasoning(done, 'progressive')).toBe(false);
    expect(done.collapsedSummary).toContain('researcher');
  });

  it('keeps all reasoning expanded under Paranoid policy', () => {
    const done = finalizeReasoningItem(reasoningItem);
    expect(shouldExpandReasoning(done, 'paranoid')).toBe(true);
  });

  it('collapses non-live reasoning under Velocity policy', () => {
    const done = finalizeReasoningItem(reasoningItem);
    expect(shouldExpandReasoning(done, 'velocity')).toBe(false);
    expect(shouldExpandReasoning(reasoningItem, 'velocity')).toBe(true);
  });

  it('respects user-expanded override', () => {
    const done = finalizeReasoningItem(reasoningItem);
    expect(shouldExpandReasoning(done, 'progressive', true)).toBe(true);
  });

  it('builds collapsed summary with tool call count', () => {
    const summary = buildCollapsedSummary(finalizeReasoningItem(reasoningItem));
    expect(summary).toMatch(/tool call/);
  });

  it('collapseAllReasoning marks all reasoning items decisive', () => {
    const items = collapseAllReasoning([reasoningItem]);
    expect(items[0].decisive).toBe(true);
    expect(items[0].inFlight).toBe(false);
  });

  it('human messages always expanded', () => {
    const human: FeedItem = { ...reasoningItem, kind: 'human_message', from: 'user' };
    expect(shouldExpandReasoning(human, 'velocity')).toBe(true);
  });
});