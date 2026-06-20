import { test, expect } from '@playwright/test';

test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

async function openMainChannel(page) {
  await page.goto('/#channels');
  await expect(page.getByTestId('channels-panel')).toBeVisible({ timeout: 8000 });
  const first = page.getByTestId('channels-list').locator('li').first();
  if (await first.isVisible()) await first.click();
  await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 5000 });
}

test.describe('Progressive reasoning & policy (fixture)', () => {
  test('policy preset toggle switches between Progressive and Paranoid', async ({ page }) => {
    await waitPortalReady(page);
    await openMainChannel(page);

    const viewport = page.viewportSize();
    const isMobile = viewport && viewport.width <= 768;
    if (isMobile) {
      await page.getByRole('button', { name: 'Context' }).click();
      await expect(page.getByTestId('bottom-sheet')).toBeVisible({ timeout: 5000 });
    } else {
      await expect(page.getByTestId('context-panel')).toBeVisible({ timeout: 5000 });
    }

    await expect(page.getByTestId('policy-preset-toggle')).toBeVisible({ timeout: 5000 });
    await page.getByTestId('policy-progressive').click();
    await expect(page.getByTestId('policy-progressive')).toHaveAttribute('aria-pressed', 'true');

    await page.getByTestId('policy-paranoid').click();
    await expect(page.getByTestId('policy-paranoid')).toHaveAttribute('aria-pressed', 'true');

    await page.getByTestId('policy-velocity').click();
    await expect(page.getByTestId('policy-velocity')).toHaveAttribute('aria-pressed', 'true');
  });

  test('Agent Activity Summary visible on Channels', async ({ page }) => {
    await waitPortalReady(page);
    await openMainChannel(page);
    const viewport = page.viewportSize();
    const isMobile = viewport && viewport.width <= 768;
    if (isMobile) {
      await expect(page.getByTestId('agent-activity-summary')).toBeVisible();
    } else {
      await expect(page.getByTestId('context-panel').getByTestId('agent-activity-summary')).toBeVisible();
    }
  });

  test('collapse all reasoning control present on channel feed', async ({ page }) => {
    await waitPortalReady(page);
    await openMainChannel(page);
    await expect(page.getByTestId('collapse-all-reasoning')).toBeVisible();
  });

  test('global policy in Settings', async ({ page }) => {
    await waitPortalReady(page);
    await page.goto('/#settings');
    await expect(page.getByTestId('settings-panel')).toBeVisible({ timeout: 8000 });
    await expect(page.getByTestId('policy-preset-toggle')).toBeVisible();
  });
});