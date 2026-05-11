import { test, expect } from '@playwright/test';

test.describe('User Journey E2E Tests', () => {
  test('User Journey 1: Onboarding and basic chat', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Chat with AegisClaw' })).toBeVisible();

    const input = page.locator('#messageInput');
    await input.fill('What is AegisClaw?');
    await page.getByTestId('send-button').click();

    await expect(page.locator('#messages .message-bubble.user')).toContainText('What is AegisClaw?');
    await expect(page.locator('#messages .message-bubble.agent').last()).toContainText(/Ollama|AegisClaw|backend|unavailable/i);
  });

  test('User Journey 2: Skill discovery and usage', async ({ page }) => {
    await page.goto('/');

    await page.getByRole('button', { name: 'Skills' }).click();
    await expect(page.getByTestId('skills-panel')).toContainText('Discord Monitor');
    await expect(page.getByTestId('skills-panel')).toContainText('network:discord.com');
    await expect(page.getByTestId('propose-skill-button')).toBeVisible();
  });

  test('User Journey 3: Proposal creation and tracking', async ({ page }) => {
    await page.goto('/');

    await page.getByRole('button', { name: 'Court' }).click();
    await expect(page.getByTestId('court-panel')).toContainText('UNDER REVIEW');
    await expect(page.getByTestId('court-panel')).toContainText('APPROVED');
    await expect(page.getByTestId('court-panel')).toContainText('Pending backend security gates');
  });

  test('User Journey 4: Court review monitoring', async ({ page }) => {
    await page.goto('/');

    await page.getByRole('button', { name: 'Monitoring' }).click();
    await expect(page.getByTestId('monitoring-panel')).toContainText('Running VMs');
    await expect(page.getByTestId('monitoring-panel')).toContainText('unknown');
    await expect(page.getByTestId('safe-mode-toggle')).toBeVisible();
  });
});
