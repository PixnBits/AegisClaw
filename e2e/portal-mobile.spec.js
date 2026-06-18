import { test, expect } from '@playwright/test';

test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

const MOBILE = { viewport: { width: 390, height: 844 } };

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

test.describe('Mobile layout (fixture)', () => {
  test.use(MOBILE);

  test('bottom navigation visible with Channels, Dashboard, Court, More', async ({ page }) => {
    await waitPortalReady(page);
    await expect(page.getByTestId('bottom-nav')).toBeVisible();
    await expect(page.getByTestId('bottom-nav-channels')).toBeVisible();
    await expect(page.getByTestId('bottom-nav-dashboard')).toBeVisible();
    await expect(page.getByTestId('bottom-nav-court')).toBeVisible();
    await expect(page.getByTestId('bottom-nav-more')).toBeVisible();
  });

  test('navigate via bottom nav to Dashboard', async ({ page }) => {
    await waitPortalReady(page);
    await page.getByTestId('bottom-nav-dashboard').click();
    await expect(page.getByTestId('dashboard-panel')).toBeVisible({ timeout: 8000 });
  });

  test('Agent Activity Summary on mobile channels', async ({ page }) => {
    await waitPortalReady(page);
    await page.getByTestId('bottom-nav-channels').click();
    const first = page.getByTestId('channels-list').locator('li').first();
    if (await first.isVisible()) await first.click();
    await expect(page.getByTestId('agent-activity-summary')).toBeVisible({ timeout: 5000 });
  });

  test('context bottom sheet opens on mobile', async ({ page }) => {
    await waitPortalReady(page);
    await page.goto('/#channels');
    const first = page.getByTestId('channels-list').locator('li').first();
    if (await first.isVisible()) await first.click();
    const contextBtn = page.getByRole('button', { name: 'Context' });
    if (await contextBtn.isVisible()) {
      await contextBtn.click();
      await expect(page.getByTestId('bottom-sheet')).toBeVisible();
    }
  });
});

test.describe('Visual regression snapshots', () => {
  test('desktop home layout', async ({ page }) => {
    await waitPortalReady(page);
    await expect(page.getByTestId('home-panel')).toHaveScreenshot('home-desktop.png', {
      maxDiffPixelRatio: 0.05,
    });
  });

  test.use(MOBILE);

  test('mobile channels layout', async ({ page }) => {
    await waitPortalReady(page);
    await page.getByTestId('bottom-nav-channels').click();
    await expect(page.getByTestId('channel-primary')).toBeVisible({ timeout: 8000 });
    await expect(page.getByTestId('channel-primary')).toHaveScreenshot('channels-mobile.png', {
      maxDiffPixelRatio: 0.05,
    });
  });
});