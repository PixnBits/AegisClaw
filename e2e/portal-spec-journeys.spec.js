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
    const first = page.locator('[data-testid="agents-list"] .list-card').first();
    if (await first.isVisible()) {
      await first.click();
      await expect(page.getByTestId('trace-panel')).toBeVisible();
      await expect(page.getByTestId('trace-timeline')).toBeVisible();
    }
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