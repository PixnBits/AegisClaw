import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.skip(!process.env.AEGIS_E2E_FIXTURE, 'Run via make test-e2e-contract');

const VIEWPORTS = {
  mobile: { width: 390, height: 844 },
  tablet: { width: 834, height: 1112 },
  desktop: { width: 1280, height: 800 },
};

/** Common phone widths for framing QA at 100% zoom */
const MOBILE_FRAMING_VIEWPORTS = {
  iphoneSE: { width: 375, height: 667 },
  iphone14: { width: 390, height: 844 },
  pixel7: { width: 412, height: 915 },
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
      const shotTarget =
        name === 'mobile' ? page.getByTestId('channel-primary') : page.getByTestId('channel-detail');
      await expect(shotTarget).toBeVisible({ timeout: 5000 });
      // Note: feed content is dynamic across parallel tests; visual baseline covered by "Channels empty".
      // We still exercise the active layout here for structure.
    });

    test(`Channels empty (${name})`, async ({ page }) => {
      await showChannelsEmpty(page);
      await expect(page.getByTestId('channels-panel')).toHaveScreenshot(`channels-empty-${name}.png`, {
        maxDiffPixelRatio: 0.08,
      });
    });
  });
}

for (const [name, viewport] of Object.entries(MOBILE_FRAMING_VIEWPORTS)) {
  test.describe(`Mobile viewport framing — ${name}`, () => {
    test.use({ viewport });

    test(`Home full shell (${name})`, async ({ page }) => {
      await waitPortalReady(page);
      await expect(page.getByTestId('app-shell')).toHaveScreenshot(`viewport-home-${name}.png`, {
        maxDiffPixelRatio: 0.08,
      });
    });

    test(`Channels full shell (${name})`, async ({ page }) => {
      await waitPortalReady(page);
      await openMainChannel(page);
      await expect(page.getByTestId('app-shell')).toHaveScreenshot(`viewport-channels-${name}.png`, {
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
      .disableRules(['color-contrast', 'scrollable-region-focusable'])
      .analyze();
    const critical = results.violations.filter((v) => v.impact === 'critical' || v.impact === 'serious');
    expect(critical).toEqual([]);
  });
});