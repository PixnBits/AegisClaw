import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

const VIEWPORTS = {
  mobile: { width: 390, height: 844 },
  tablet: { width: 834, height: 1112 },
  desktop: { width: 1280, height: 800 },
};

async function waitPortalReady(page) {
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

async function openMainChannel(page) {
  await page.goto('/#channels');
  await expect(page.getByTestId('channels-panel')).toBeVisible({ timeout: 8000 });
  await expect(page.getByTestId('channel-detail')).toBeVisible({ timeout: 8000 });
}

async function showChannelsEmpty(page) {
  await waitPortalReady(page);
  await page.goto('/#channels');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
  await page.evaluate(() => {
    window.__portalStore?.setState({ currentChannel: null, skipChannelAutoSelect: true });
  });
  await expect(page.getByTestId('channel-empty-state')).toBeVisible({ timeout: 5000 });
}

for (const [name, viewport] of Object.entries(VIEWPORTS)) {
  test.describe(`Visual polish — ${name}`, () => {
    test.use({ viewport });

    test(`Home (${name})`, async ({ page }) => {
      await waitPortalReady(page);
      await expect(page.getByTestId('home-panel')).toHaveScreenshot(`home-${name}.png`, {
        maxDiffPixelRatio: 0.08,
      });
    });

    test(`Channels active (${name})`, async ({ page }) => {
      await waitPortalReady(page);
      await openMainChannel(page);
      await expect(page.getByTestId('channel-detail')).toHaveScreenshot(`channels-active-${name}.png`, {
        maxDiffPixelRatio: 0.08,
      });
    });

    test(`Channels empty (${name})`, async ({ page }) => {
      await showChannelsEmpty(page);
      await expect(page.getByTestId('channels-panel')).toHaveScreenshot(`channels-empty-${name}.png`, {
        maxDiffPixelRatio: 0.08,
      });
    });
  });
}

test.describe('Accessibility', () => {
  test('Channels view has no critical axe violations', async ({ page }) => {
    await waitPortalReady(page);
    await openMainChannel(page);
    const results = await new AxeBuilder({ page })
      .disableRules(['color-contrast'])
      .analyze();
    const critical = results.violations.filter((v) => v.impact === 'critical' || v.impact === 'serious');
    expect(critical).toEqual([]);
  });
});