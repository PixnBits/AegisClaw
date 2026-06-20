import { test, expect } from '@playwright/test';

// Regression: agent store posts must appear in REST + STOMP after a user portal post.
// Invoked from scripts/verify-channel-agent-replies-e2e.sh after the store gate passes.
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Requires real daemon');
test.skip(!process.env.AEGIS_E2E_LIVE_STOMP, 'Set AEGIS_E2E_LIVE_STOMP=1 with running daemon');

const CHANNEL = process.env.AEGIS_PORTAL_CHANNEL || 'main';
const MARKER = process.env.AEGIS_CHANNEL_REPLY_MARKER || `channel-reply-${Date.now()}`;
const MIN_REPLIES = Number(process.env.AEGIS_CHANNEL_MIN_AGENT_REPLIES || '2');

const AGENT_FROM = /^(project-manager|court-persona-[a-z0-9-]+)/;

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 20000 });
  await expect(page.getByTestId('connection-status-label')).toContainText(/STOMP|SSE/, { timeout: 15000 });
}

test.describe('Channel agent reply pipeline (regression)', () => {
  test('store agent replies visible in REST, UI, and STOMP', async ({ page, request }) => {
    const apiRes = await request.get(`/api/channels/${CHANNEL}`);
    expect(apiRes.ok()).toBeTruthy();
    const body = await apiRes.json();
    const messages = body.messages || [];

    const userPost = messages.find(
      (m) =>
        String(m.content || '').includes(MARKER) &&
        ['user', 'operator', 'web-portal', 'portal', 'cli'].includes(String(m.from || '').toLowerCase()),
    );
    expect(userPost, 'user marker post in store').toBeTruthy();

    const agentReplies = messages.filter(
      (m) =>
        AGENT_FROM.test(String(m.from || '')) &&
        String(m.content || '').trim().length > 0 &&
        !String(m.content || '').match(/^I'm the .+\. I (evaluate|assess|review|coordinate)/i) &&
        !String(m.content || '').trim().toUpperCase().startsWith('NO_REPLY'),
    );
    expect(agentReplies.length, 'agent replies in store').toBeGreaterThanOrEqual(MIN_REPLIES);

    const stompPayloads = [];
    page.on('websocket', (ws) => {
      if (!ws.url().includes('/stomp')) return;
      ws.on('framereceived', (frame) => {
        const text = String(frame.payload || '');
        if (text.startsWith('MESSAGE') && text.includes('channel.activity')) {
          const bodyIdx = text.indexOf('\n\n');
          if (bodyIdx >= 0) {
            stompPayloads.push(text.slice(bodyIdx + 2).replace(/\0/g, ''));
          }
        }
      });
    });

    await waitPortalReady(page);
    await page.getByTestId('nav-channels').click();
    await expect(page.getByTestId('channels-panel')).toBeVisible({ timeout: 10000 });

    const channelItem = page.getByTestId('channels-list').getByText(CHANNEL, { exact: false }).first();
    await channelItem.click();
    await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 10000 });

    const messagesEl = page.locator('[data-testid="channel-messages"]');
    await expect(messagesEl).toContainText(MARKER, { timeout: 15000 });
    await expect(messagesEl).toContainText(agentReplies[0].content.slice(0, 24), { timeout: 15000 });

    const followUp = `${MARKER}-stomp: Please reply with one short sentence.`;
    const postRes = await request.post(`/api/channels/${CHANNEL}`, {
      data: { from: 'user', content: followUp },
    });
    expect(postRes.ok()).toBeTruthy();

    await expect.poll(
      () => {
        for (const raw of stompPayloads) {
          try {
            const payload = JSON.parse(raw);
            if (payload.type !== 'channel.activity') continue;
            let event = payload.event;
            if (typeof event === 'string') {
              event = JSON.parse(event);
            }
            const from = String(event?.from || '');
            if (AGENT_FROM.test(from)) {
              return from;
            }
          } catch {
            /* ignore */
          }
        }
        return null;
      },
      { timeout: 120_000, message: 'waiting for agent channel.activity STOMP frame after follow-up post' },
    ).not.toBeNull();
  });
});
