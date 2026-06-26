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
  await expect(page.getByTestId('channel-messages')).toBeVisible();
  await page.waitForFunction(
    () =>
      document.querySelector('[data-testid="connection-status-chip"]')?.getAttribute('data-connection-mode') ===
      'stomp',
    { timeout: 5000 },
  );
}

test.describe('Web Portal real-time (fixture)', () => {
  test('multi-tab channel updates via STOMP', async ({ browser }) => {
    const context = await browser.newContext();
    const pageA = await context.newPage();
    const pageB = await context.newPage();

    await waitPortalReady(pageA);
    await waitPortalReady(pageB);
    await expect(pageA.getByTestId('connection-status-label')).toContainText('STOMP', { timeout: 10000 });
    await expect(pageB.getByTestId('connection-status-label')).toContainText('STOMP', { timeout: 10000 });
    await openMainChannel(pageA);
    await openMainChannel(pageB);

    const marker = `multi-tab-${Date.now()}`;
    const postRes = await pageA.request.post('/api/channels/main', {
      data: { from: 'user', content: marker },
    });
    expect(postRes.ok()).toBeTruthy();

    await expect(pageB.getByTestId('channel-messages')).toContainText(marker, { timeout: 15000 });
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

  test('shows disconnected when STOMP and SSE are unavailable', async ({ browser }) => {
    const context = await browser.newContext();
    await context.routeWebSocket(/\/stomp$/, (ws) => ws.close());
    await context.route('**/events', (route) => route.abort());
    const page = await context.newPage();
    await waitPortalReady(page);
    await expect(page.getByTestId('connection-status-label')).toContainText('Disconnected', {
      timeout: 10000,
    });
    await expect(page.getByTestId('connection-status-chip')).toHaveAttribute(
      'data-connection-mode',
      'disconnected',
    );
    await context.close();
  });

  test('member invite and remove updates grouped list', async ({ page }) => {
    await waitPortalReady(page);
    await openMainChannel(page);

    const role = `coder-${Date.now()}`;
    await page.getByTestId('members-section').getByRole('button', { name: /Members/i }).click();
    await page.getByTestId('toggle-invite-button').click();
    await expect(page.getByTestId('add-member-form')).toBeVisible({ timeout: 5000 });
    await page.getByTestId('add-member-input').fill(role);
    await page.getByTestId('add-member-button').click();

    const group = page.getByTestId('member-group-project-sdlc');
    await group.getByRole('button', { name: /Project \/ SDLC/i }).click();
    const memberRow = page.getByTestId(`member-${role}`);
    await expect(memberRow).toBeVisible({ timeout: 5000 });

    await memberRow.getByRole('button', { name: `Actions for ${role}` }).click();
    page.once('dialog', (dialog) => dialog.accept());
    await memberRow.getByRole('button', { name: 'Remove' }).click();

    await expect(page.getByTestId(`member-${role}`)).toHaveCount(0, { timeout: 5000 });
  });
});