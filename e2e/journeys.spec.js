import { test, expect } from '@playwright/test';

test.describe('User Journey E2E Tests', () => {
  test('User Journey 1: Onboarding and basic chat', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Chat with AegisClaw' })).toBeVisible();

    const input = page.locator('#messageInput');
    await input.fill('What is AegisClaw?');
    await page.locator('button:has-text("Send")').click();

    await expect(page.locator('#messages .message.user')).toContainText('What is AegisClaw?');
    await expect(page.locator('#messages .message.agent').last()).toContainText('What is AegisClaw?');
  });

  test('User Journey 2: Skill discovery and usage', async ({ page }) => {
    test.skip(true, 'Phase 0 only automates journey 1; skill workflow UI is not implemented yet.');
  });

  test('User Journey 3: Proposal creation and tracking', async ({ page }) => {
    test.skip(true, 'Phase 0 only automates journey 1; proposal workflow UI is not implemented yet.');
  });

  test('User Journey 4: Court review monitoring', async ({ page }) => {
    test.skip(true, 'Phase 0 only automates journey 1; court review UI is not implemented yet.');
  });
});
