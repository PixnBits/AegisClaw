import { test, expect } from '@playwright/test';

// Real-time portal journeys per docs/specs/web-portal/test-e2e.md
test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

async function openMainChannel(page) {
  await page.goto('/#channels');
  await expect(page.getByTestId('channels-panel')).toBeVisible({ timeout: 8000 });
  const first = page.getByTestId('channels-list').locator('li').first();
  await first.click();
  await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 5000 });
}

test.describe('Web Portal real-time (fixture)', () => {
  test('multi-tab channel updates via STOMP', async ({ browser }) => {
    const context = await browser.newContext();
    const pageA = await context.newPage();
    const pageB = await context.newPage();

    await waitPortalReady(pageA);
    await waitPortalReady(pageB);
    await openMainChannel(pageA);
    await openMainChannel(pageB);

    const marker = `multi-tab-${Date.now()}`;
    const postRes = await pageA.request.post('/api/channels/main', {
      data: { from: 'user', content: marker },
    });
    expect(postRes.ok()).toBeTruthy();

    await expect(pageB.getByTestId('channel-messages')).toContainText(marker, { timeout: 8000 });
    await context.close();
  });

  test('STOMP disconnect falls back to SSE', async ({ browser }) => {
    const context = await browser.newContext();
    await context.routeWebSocket(/\/stomp$/, (ws) => ws.close());
    const page = await context.newPage();
    await waitPortalReady(page);
    await expect(page.getByTestId('connection-status-label')).toContainText('SSE', { timeout: 10000 });
    await expect(page.getByTestId('app-shell')).toBeVisible();
    await context.close();
  });

  test('member invite and remove updates grouped list', async ({ page }) => {
    await waitPortalReady(page);
    await openMainChannel(page);

    const role = `coder-${Date.now()}`;
    await page.getByTestId('toggle-invite-button').click();
    await expect(page.getByTestId('add-member-form')).toBeVisible({ timeout: 5000 });
    await page.getByTestId('add-member-input').fill(role);
    await page.getByTestId('add-member-button').click();

    await expect(page.getByTestId('members-list')).toContainText(role, { timeout: 5000 });

    const removeBtn = page.getByTestId(`member-${role}`).getByRole('button', { name: 'Remove' });
    page.once('dialog', (dialog) => dialog.accept());
    await removeBtn.click();

    await expect(page.getByTestId('members-list')).not.toContainText(role, { timeout: 5000 });
  });
});