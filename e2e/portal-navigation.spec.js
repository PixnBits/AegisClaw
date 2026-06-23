import { test, expect } from '@playwright/test';

test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
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
    await expect(page.getByTestId('connection-status-label')).toContainText('STOMP', { timeout: 10000 });
    await page.goto('/#channels');
    const first = page.getByTestId('channels-list').locator('li').first();
    await first.click();
    await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 5000 });

    const marker = `md-e2e-${Date.now()}`;
    const body = `**Bold** and \`code\` — ${marker}`;
    const postRes = await page.request.post('/api/channels/main', {
      data: { from: 'user', content: body },
    });
    expect(postRes.ok()).toBeTruthy();

    const feed = page.getByTestId('channel-messages');
    await expect(feed).toContainText(marker, { timeout: 8000 });
    await expect(feed.locator('.markdown-content strong')).toContainText('Bold');
    await expect(feed.locator('.markdown-content code')).toContainText('code');
  });

  test('STOMP delivers posted message without page reload', async ({ page }) => {
    await waitPortalReady(page);
    await expect(page.getByTestId('connection-status-label')).toContainText('STOMP', { timeout: 10000 });
    await page.goto('/#channels');
    const first = page.getByTestId('channels-list').locator('li').first();
    await first.click();
    await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 5000 });

    const feed = page.getByTestId('channel-messages');
    const marker = `stomp-inline-${Date.now()}`;
    const postRes = await page.request.post('/api/channels/main', {
      data: { from: 'user', content: marker },
    });
    expect(postRes.ok()).toBeTruthy();

    await expect(feed).toContainText(marker, { timeout: 8000 });
  });
});
