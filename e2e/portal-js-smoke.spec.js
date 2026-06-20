import { test, expect } from '@playwright/test';

test('portal JS modules load without errors', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text());
  });
  await page.goto('/');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
  expect(errors, `JS errors: ${errors.join('; ')}`).toEqual([]);
});

test('brand logo navigates to Home', async ({ page }) => {
  await page.goto('/#channels');
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
  await page.getByTestId('brand-home').click();
  await expect(page.getByTestId('home-panel')).toBeVisible({ timeout: 8000 });
  expect(page.url()).toMatch(/#home/);
});