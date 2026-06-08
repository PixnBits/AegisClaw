import { test, expect } from '@playwright/test';

// Real daemon E2E for collaboration model (channels + PM + LLM posts).
// Run as part of make test-e2e-llm (after CLI pm goal has posted to channel).
// Skips in fixture mode; use real daemon + portal at :8080.
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Collaboration browser checks require real daemon (use make test-e2e-llm after start)');

test.describe('Collaboration E2E (browser verification of channels/PM posts)', () => {
  test('Channels UI shows PM plan post (after CLI pm goal with E2E-LLM-VERIFY)', async ({ page }) => {
    // Navigate to channels (primary collab view, replaced old chat).
    await page.goto('/#channels');

    // The channel created/posted by the E2E CLI (plan-demo-e2e-llm) should be visible.
    // Use text or list item (UI renders channelsList with id and members).
    const channelItem = page.getByText('plan-demo-e2e-llm').first();
    await expect(channelItem).toBeVisible({ timeout: 15000 });

    // Select the channel (click the list item).
    await channelItem.click();

    // Channel detail should show messages area.
    const messages = page.locator('#channelMessages');
    await expect(messages).toBeVisible({ timeout: 10000 });

    // The PM post from the goal should be present: contains the verify string and from project-manager.
    // Content is rendered as <div class="message"> or inner text with from + content.
    await expect(messages).toContainText('E2E-LLM-VERIFY', { timeout: 15000 });
    await expect(messages).toContainText('project-manager', { timeout: 10000 });

    // Also check default 'main' channel has Court/PM members or activity (E2E defaults).
    await page.goto('/#channels');
    await expect(page.getByText('main')).toBeVisible({ timeout: 10000 });
  });
});
