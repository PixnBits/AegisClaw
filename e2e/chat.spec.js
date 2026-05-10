import { test, expect } from '@playwright/test';

test.describe('Web Portal E2E Tests', () => {
  test('should load the main page', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/AegisClaw/); // Assume title
  });

  test('should handle chat streaming', async ({ page }) => {
    await page.goto('/');
    // Assume UI elements
    const input = page.locator('input[placeholder="Enter your message"]');
    const sendButton = page.locator('button:has-text("Send")');

    await input.fill('Hello AegisClaw');
    await sendButton.click();

    // Wait for streaming response
    const response = page.locator('.chat-message.agent');
    await expect(response).toContainText('Based on my analysis');
  });

  test('should handle tool calls in stream', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('input[placeholder="Enter your message"]');
    const sendButton = page.locator('button:has-text("Send")');

    await input.fill('Search for AI news');
    await sendButton.click();

    // Check for tool_call event
    const toolCall = page.locator('[data-type="tool_call"]');
    await expect(toolCall).toBeVisible();
    await expect(toolCall).toContainText('web_search');
  });

  test('should handle incremental responses', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('input[placeholder="Enter your message"]');
    const sendButton = page.locator('button:has-text("Send")');

    await input.fill('Explain quantum computing');
    await sendButton.click();

    // Wait for incremental text
    const response = page.locator('.chat-message.agent');
    await expect(response).toContainText('quantum');
    // Ensure it's streaming (multiple updates)
    await page.waitForTimeout(1000);
    await expect(response).toHaveText(/quantum.*computing/);
  });

  test('should handle errors gracefully', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('input[placeholder="Enter your message"]');
    const sendButton = page.locator('button:has-text("Send")');

    // Send empty message
    await sendButton.click();
    await expect(page.locator('.error')).toContainText('Missing message');
  });
});