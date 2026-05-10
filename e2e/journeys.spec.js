import { test, expect } from '@playwright/test';

test.describe('User Journey E2E Tests', () => {
  test('User Journey 1: Onboarding and basic chat', async ({ page }) => {
    await page.goto('/');
    // Onboarding flow
    await expect(page.locator('h1')).toContainText('Welcome to AegisClaw');
    await page.locator('button:has-text("Get Started")').click();

    // Basic chat
    const input = page.locator('input[placeholder="Ask me anything"]');
    await input.fill('What is AegisClaw?');
    await page.locator('button:has-text("Send")').click();

    await expect(page.locator('.chat-response')).toContainText('AegisClaw');
  });

  test('User Journey 2: Skill discovery and usage', async ({ page }) => {
    await page.goto('/skills');
    await expect(page.locator('.skill-list')).toBeVisible();

    // Select a skill
    await page.locator('.skill-card').first().click();
    await page.locator('button:has-text("Use Skill")').click();

    // Chat with skill
    const input = page.locator('input[placeholder="Enter your request"]');
    await input.fill('Help me with this task');
    await page.locator('button:has-text("Submit")').click();

    await expect(page.locator('.skill-response')).toBeVisible();
  });

  test('User Journey 3: Proposal creation and tracking', async ({ page }) => {
    await page.goto('/proposals');
    await page.locator('button:has-text("Create Proposal")').click();

    await page.locator('input[name="title"]').fill('Add new feature');
    await page.locator('textarea[name="description"]').fill('This feature would...');
    await page.locator('button:has-text("Submit")').click();

    await expect(page.locator('.proposal-status')).toContainText('Pending Review');
  });

  test('User Journey 4: Court review monitoring', async ({ page }) => {
    await page.goto('/proposals/123'); // Assume proposal ID
    await expect(page.locator('.court-votes')).toBeVisible();
    await expect(page.locator('.court-votes')).toContainText('CISO: Approve');
  });
});