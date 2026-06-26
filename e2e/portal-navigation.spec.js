import { test, expect } from '@playwright/test';

test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

/** Wait for STOMP connect + channel topic subscription after opening a channel. */
async function waitChannelStompReady(page) {
  await expect(page.getByTestId('connection-status-label')).toContainText('STOMP', { timeout: 10000 });
  await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 5000 });
  await expect(page.getByTestId('channel-messages')).toBeVisible();
  await page.waitForFunction(
    () =>
      document.querySelector('[data-testid="connection-status-chip"]')?.getAttribute('data-connection-mode') ===
      'stomp',
    { timeout: 5000 },
  );
}

test.describe('Portal navigation (fixture)', () => {
  test('brand logo navigates to Home from Channels', async ({ page }) => {
    await waitPortalReady(page);
    await page.goto('/#channels');
    await expect(page.getByTestId('channels-panel')).toBeVisible({ timeout: 8000 });
    await page.getByTestId('brand-home').click();
    await expect(page.getByTestId('home-panel')).toBeVisible({ timeout: 5000 });
    expect(page.url()).toMatch(/#home/);
  });

  test('markdown renders in channel feed', async ({ page }) => {
    await waitPortalReady(page);
    await page.goto('/#channels');
    const first = page.getByTestId('channels-list').locator('li').first();
    await first.click();
    await waitChannelStompReady(page);

    const marker = `md-e2e-${Date.now()}`;
    const boldToken = `MdBold${marker}`;
    const codeToken = `MdCode${marker}`;
    const body = `**${boldToken}** and \`${codeToken}\` — ${marker}`;
    const feed = page.getByTestId('channel-messages');
    await page.getByTestId('message-input').fill(body);
    await page.getByTestId('send-button').click();
    await expect(feed).toContainText(marker, { timeout: 15000 });
    const posted = feed.locator('article').filter({ hasText: marker });
    await expect(posted.locator('.markdown-content strong')).toContainText(boldToken);
    await expect(posted.locator('.markdown-content code')).toContainText(codeToken);
  });

  test('STOMP delivers posted message without page reload', async ({ page }) => {
    await waitPortalReady(page);
    // Fresh WS session after long contract suites (avoids stale STOMP subscriptions).
    await page.reload();
    await waitPortalReady(page);
    await page.goto('/#channels');
    const first = page.getByTestId('channels-list').locator('li').first();
    await first.click();
    await waitChannelStompReady(page);

    const feed = page.getByTestId('channel-messages');
    const marker = `stomp-inline-${Date.now()}`;
    const postRes = await page.request.post('/api/channels/main', {
      data: { from: 'user', content: marker },
    });
    expect(postRes.ok()).toBeTruthy();

    await expect(feed).toContainText(marker, { timeout: 15000 });
  });
});
