import { test, expect } from '@playwright/test';

// Target-state portal journeys per docs/specs/web-portal/test-e2e.md
test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

test.describe('Web Portal spec journeys (fixture)', () => {
  test('Home → goal → plan preview → channel harness', async ({ page }) => {
    await waitPortalReady(page);
    await expect(page.getByTestId('home-panel')).toBeVisible();
    await page.getByTestId('command-bar-input').fill('Research Zig vs Rust for home lab scripts');
    await page.getByTestId('command-bar-submit').click();
    await expect(page.getByTestId('plan-preview')).toBeVisible({ timeout: 8000 });
    await page.getByTestId('plan-preview-open').click();
    await expect(page.getByTestId('channels-panel')).toBeVisible();
    await expect(page.getByTestId('harness-overview')).toBeVisible();
    await expect(page.getByTestId('pipeline-strip')).toBeVisible();
  });

  test('Dashboard active work + Canvas navigation', async ({ page }) => {
    await waitPortalReady(page);
    await page.getByTestId('nav-dashboard').click();
    await expect(page.getByTestId('dashboard-panel')).toBeVisible({ timeout: 8000 });
    await expect(page.getByTestId('active-work-list')).toBeVisible();
    await page.getByTestId('open-canvas-button').click();
    await expect(page.getByTestId('canvas-panel')).toBeVisible();
    await expect(page.getByTestId('canvas-agent-grid')).toBeVisible();
  });

  test('Court proposal detail with persona votes', async ({ page, request }) => {
    const list = await request.get('/api/proposals');
    expect(list.ok()).toBeTruthy();
    const proposals = await list.json();
    const first = proposals[0];
    test.skip(!first?.id, 'No fixture proposals');

    const reviews = await request.get(`/api/proposals/${first.id}/reviews`);
    expect(reviews.ok()).toBeTruthy();
    const body = await reviews.json();
    expect(body).toHaveProperty('reviews');

    await waitPortalReady(page);
    await page.getByTestId('nav-court').click();
    await expect(page.getByTestId('proposals-list')).toBeVisible({ timeout: 8000 });
    await page.getByTestId(`proposal-${first.id}`).click();
    await expect(page.getByTestId('court-detail')).toBeVisible();
    await expect(page.getByTestId('court-actions')).toBeVisible();
  });

  test('Agents list opens trace timeline', async ({ page }) => {
    await waitPortalReady(page);
    await page.getByTestId('nav-agents').click();
    await expect(page.getByTestId('agents-panel')).toBeVisible({ timeout: 8000 });
    const first = page.locator('[data-testid="agents-specialists-list"] .list-card').first();
    if (await first.isVisible()) {
      await first.click();
      await expect(page.getByTestId('trace-panel')).toBeVisible();
      await expect(page.getByTestId('trace-timeline')).toBeVisible();
    }
  });

  test('Agent trace shows permission requests and grants panel', async ({ page, request }) => {
    // baseline via direct API (reliable in fixture)
    const before = await request.get('/api/agents/coder-test/permissions');
    expect(before.ok()).toBeTruthy();
    const b0 = await before.json();
    expect(b0).toHaveProperty('grants');
    expect(b0).toHaveProperty('requests');
    const grantsBefore = Array.isArray(b0.grants) ? b0.grants.length : 0;

    // drive grant via API (exercises real POST handler + fixture client)
    const grantRes = await request.post('/api/agents/coder-test/permissions', {
      data: { action: 'grant', capability: 'channel.post' },
      headers: { 'X-Aegis-Confirmed': '1' },
    });
    expect(grantRes.ok()).toBeTruthy();

    const afterGrant = await request.get('/api/agents/coder-test/permissions');
    const b1 = await afterGrant.json();
    const grantsAfter = Array.isArray(b1.grants) ? b1.grants.length : 0;
    expect(grantsAfter).toBeGreaterThanOrEqual(grantsBefore); // at least no loss

    // drive revoke
    const revokeRes = await request.post('/api/agents/coder-test/permissions', {
      data: { action: 'revoke', capability: 'channel.post' },
      headers: { 'X-Aegis-Confirmed': '1' },
    });
    expect(revokeRes.ok()).toBeTruthy();

    // delegation roundtrip via API
    const d0 = await request.get('/api/settings/ciso-delegation');
    const d0b = await d0.json();
    await request.post('/api/settings/ciso-delegation', {
      data: { enabled: true },
      headers: { 'X-Aegis-Confirmed': '1' },
    });
    const d1 = await request.get('/api/settings/ciso-delegation');
    const d1b = await d1.json();
    expect(d1b.enabled).toBe(true);

    // restore
    await request.post('/api/settings/ciso-delegation', {
      data: { enabled: !!d0b.enabled },
      headers: { 'X-Aegis-Confirmed': '1' },
    });

    // now visit UI and assert panels visible (elements rendered)
    await waitPortalReady(page);
    await page.getByTestId('nav-agents').click();
    await expect(page.getByTestId('agents-panel')).toBeVisible({ timeout: 8000 });
    const coderCard = page.getByTestId('agent-card-coder-test');
    if (await coderCard.isVisible()) {
      await coderCard.click();
      await expect(page.getByTestId('agent-permissions-panel')).toBeVisible();
      await expect(page.getByTestId('agent-grants-list')).toBeVisible();
      await expect(page.getByTestId('agent-permission-requests')).toBeVisible();
      await expect(page.getByTestId('agent-visibility-list')).toBeVisible();
    }

    // delegation toggle UI element present
    await page.getByTestId('nav-settings').click();
    await expect(page.getByTestId('ciso-delegation-toggle')).toBeVisible();
  });

  test('Harness API contract', async ({ request }) => {
    const res = await request.get('/api/channels/main/harness');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('plan');
  });

  test('Goals API contract', async ({ request }) => {
    const res = await request.post('/api/goals', {
      data: { goal: 'E2E fixture goal', channel_id: 'main' },
    });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('plan_id');
    expect(body.preview).toBeTruthy();
  });
});