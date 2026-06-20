import { describe, expect, it } from 'vitest';
import { topicsForView, TOPIC } from './index';

describe('STOMP topic subscriptions', () => {
  it('home subscribes to overview and approvals', () => {
    const topics = topicsForView('home');
    expect(topics).toContain(TOPIC.overviewStats);
    expect(topics).toContain(TOPIC.approvalsPending);
  });

  it('channels subscribes per channel + plan', () => {
    const topics = topicsForView('channels', { channelId: 'main', planId: 'plan_main' });
    expect(topics).toContain('/topic/channel.main.activity');
    expect(topics).toContain('/topic/harness.plan_main.updates');
  });

  it('dashboard includes canvas events', () => {
    const topics = topicsForView('dashboard');
    expect(topics).toContain(TOPIC.canvasEvents);
    expect(topics).toContain(TOPIC.monitoringStats);
  });

  it('subscribes all channelIds for live updates', () => {
    const topics = topicsForView('home', { channelIds: ['main', 'ops'] });
    expect(topics).toContain('/topic/channel.main.activity');
    expect(topics).toContain('/topic/channel.ops.activity');
  });
});