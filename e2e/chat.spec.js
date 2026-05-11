import { test, expect } from '@playwright/test';

test.describe('Web Portal E2E Tests', () => {
  test('should load the secure command center shell', async ({ page }) => {
    await page.goto('/');

    await expect(page).toHaveTitle(/AegisClaw Secure Command Center/);
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Chat with AegisClaw' })).toBeVisible();
    await expect(page.getByTestId('system-status-chip')).toContainText('Daemon Running');
    await expect(page.locator('script[src^="http"],link[href^="http"]')).toHaveCount(0);
  });

  test('should render dashboard, skills, court, and monitoring data', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByTestId('dashboard-stats')).toContainText('Active Agents');
    await expect(page.getByTestId('skills-list')).toContainText('Discord Monitor');
    await expect(page.getByTestId('proposals-list')).toContainText('discord_monitor v1.2');
    await expect(page.getByTestId('monitoring-stats')).toContainText('Running VMs');
    await expect(page.getByTestId('monitoring-logs')).toContainText('researcher: Found 12 relevant papers');
  });

  test('should handle chat streaming with visible fast feedback', async ({ page }) => {
    await page.goto('/');

    const input = page.locator('#messageInput');
    const sendButton = page.getByTestId('send-button');
    const chatStatus = page.getByTestId('chat-status');

    await input.fill('Hello AegisClaw');
    const started = Date.now();
    await sendButton.click();

    await expect(chatStatus).toContainText(/Observe|Executing|Streaming/, { timeout: 1200 });
    expect(Date.now() - started).toBeLessThan(1200);

    const response = page.locator('#messages .message-bubble.agent').last();
    await expect(response).toContainText('Assessment');
    await expect(response).toContainText('Hello AegisClaw');
    await expect(response).toContainText('AegisClaw keeps the user informed');
  });

  test('should show tool calls and tool results during streaming', async ({ page }) => {
    await page.goto('/');

    await page.locator('#messageInput').fill('Search for AI security news');
    await page.getByTestId('send-button').click();

    await expect(page.locator('#messages .tool-event[data-stream-kind=\"tool-call\"]')).toContainText('tool.search');
    await expect(page.locator('#messages .tool-event[data-stream-kind=\"tool-result\"]')).toContainText('Matched secure internal guidance');
    await expect(page.locator('#recentToolsList')).toContainText('tool.search');
  });

  test('should ignore empty messages gracefully', async ({ page }) => {
    await page.goto('/');

    await page.getByTestId('send-button').click();
    await expect(page.locator('#messages .message-bubble, #messages .tool-event')).toHaveCount(0);
  });
});
