import { test, expect } from '@playwright/test';

test.describe('Web Portal E2E Tests', () => {
  test('should load the secure command center shell', async ({ page }) => {
    await page.goto('/');

    await expect(page).toHaveTitle(/Dashboard.*AegisClaw/);
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByTestId('system-status-chip')).toContainText(/Daemon (Running|Degraded)/);
    await expect(page.locator('script[src^="http"],link[href^="http"]')).toHaveCount(0);
  });

  test('should render dashboard, skills, court, and monitoring data', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByTestId('dashboard-stats')).toContainText('Active Agents');

    await page.goto('/#skills');
    await expect(page.getByTestId('skills-list')).toContainText('Discord Monitor');

    await page.goto('/#court');
    await expect(page.getByTestId('proposals-list')).toContainText('discord_monitor v1.2');

    await page.goto('/#monitoring');
    await expect(page.getByTestId('monitoring-stats')).toContainText('Running VMs');
    await expect(page.getByTestId('monitoring-logs')).toBeVisible();
  });

  test('should handle chat streaming with visible fast feedback', async ({ page }) => {
    await page.goto('/#chat');

    const input = page.locator('#messageInput');
    const sendButton = page.getByTestId('send-button');
    const chatStatus = page.getByTestId('chat-status');

    await input.fill('Hello AegisClaw');
    const started = Date.now();
    await sendButton.click();

    await expect(chatStatus).toContainText(/Observe|Executing|Streaming|unavailable/, { timeout: 1200 });
    expect(Date.now() - started).toBeLessThan(1200);

    const response = page.locator('#messages .message-bubble.agent').last();
    await expect(response).toBeVisible();
    await expect(response).toContainText(/Ollama|AegisClaw|backend|unavailable/i);
  });

  test('should show tool calls and tool results during streaming', async ({ page }) => {
    await page.goto('/#chat');

    await page.locator('#messageInput').fill('Search for AI security news');
    await page.getByTestId('send-button').click();

    await expect(page.locator('#messages .tool-event[data-stream-kind="tool-call"]')).toContainText('ollama.generate');
    await expect(page.locator('#messages .tool-event[data-stream-kind="tool-result"]')).toBeVisible();
    await expect(page.locator('#recentToolsList')).toContainText('ollama.generate');
  });

  test('should ignore empty messages gracefully', async ({ page }) => {
    await page.goto('/#chat');

    await page.getByTestId('send-button').click();
    await expect(page.locator('#messages .message-bubble, #messages .tool-event')).toHaveCount(0);
  });
});
