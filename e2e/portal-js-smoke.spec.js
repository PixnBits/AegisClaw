import { test, expect } from '@playwright/test';

test('portal JS modules load without errors', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text());
  });
  await page.goto('/');
  await page.waitForTimeout(2000);
  expect(errors, `JS errors: ${errors.join('; ')}`).toEqual([]);
  await expect(page.locator('body')).toHaveAttribute('data-portal-ready', '1');
});