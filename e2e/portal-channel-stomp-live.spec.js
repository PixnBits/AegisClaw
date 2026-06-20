import { test, expect } from '@playwright/test';

// Live daemon: agent store posts must appear on STOMP (not only the user's optimistic frame).
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Requires real daemon');
test.skip(!process.env.AEGIS_E2E_LIVE_STOMP, 'Set AEGIS_E2E_LIVE_STOMP=1 with running daemon');

const CHANNEL = process.env.AEGIS_PORTAL_CHANNEL || 'main';
const MARKER = `stomp-live-${Date.now()}`;

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 20000 });
  await expect(page.getByTestId('connection-status-label')).toContainText(/STOMP|SSE/, { timeout: 15000 });
}

test.describe('Live STOMP agent delivery', () => {
  test('agent reply publishes channel.activity after user post', async ({ page }) => {
    const stompPayloads: string[] = [];

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

    const postRes = await page.request.post(`/api/channels/${CHANNEL}`, {
      data: { from: 'user', content: `${MARKER}: Please reply with one sentence about project status.` },
    });
    expect(postRes.ok()).toBeTruthy();

    await expect.poll(
      () => {
        for (const raw of stompPayloads) {
          try {
            const payload = JSON.parse(raw) as { type?: string; event?: { from?: string; content?: string } | string };
            if (payload.type !== 'channel.activity') continue;
            let event = payload.event;
            if (typeof event === 'string') {
              event = JSON.parse(event) as { from?: string; content?: string };
            }
            const from = String(event?.from || '');
            const content = String(event?.content || '');
            if (from !== 'user' && content.includes(MARKER) === false) {
              if (from.startsWith('project-manager') || from.startsWith('court-persona-')) {
                return from;
              }
            }
          } catch {
            /* ignore */
          }
        }
        return null;
      },
      { timeout: 120_000, message: 'waiting for agent channel.activity STOMP frame' },
    ).not.toBeNull();
  });
});
