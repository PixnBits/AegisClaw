import { test, expect } from '@playwright/test';

test.describe('Web Portal E2E Tests', () => {
  test('should load the main page', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/AegisClaw/); // Assume title
  });

  test('should handle chat streaming', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('#messageInput');
    const sendButton = page.locator('button:has-text("Send")');

    await input.fill('Hello AegisClaw');
    await sendButton.click();

    const response = page.locator('#messages .message.agent').last();
    await expect(response).toContainText('Based on my analysis');
  });

  test('should handle tool calls in stream', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('#messageInput');
    const sendButton = page.locator('button:has-text("Send")');

    await input.fill('Search for AI news');
    await sendButton.click();

    const toolCall = page.locator('#messages .message.agent', { hasText: 'web_search' });
    await expect(toolCall).toBeVisible();
  });

  test('should handle incremental responses', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('#messageInput');
    const sendButton = page.locator('button:has-text("Send")');

    await input.fill('Explain quantum computing');
    await sendButton.click();

    const response = page.locator('#messages .message.agent').last();
    await expect(response).toContainText('Explain quantum computing');
    await page.waitForTimeout(500);
    expect(await page.locator('#messages .message.agent').count()).toBeGreaterThanOrEqual(4);
  });

  test('should handle errors gracefully', async ({ page }) => {
    await page.goto('/');
    const sendButton = page.locator('button:has-text("Send")');

    await sendButton.click();
    await expect(page.locator('#messages .message')).toHaveCount(0);
  });
});
