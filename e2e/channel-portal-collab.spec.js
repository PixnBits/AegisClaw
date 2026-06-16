import { test, expect } from '@playwright/test';

// Portal channel fan-out: user posts via the channels UI and agents reply.
// Run via make test-e2e-portal-channel (sets AEGIS_E2E_PORTAL_BROWSER=1 after CLI/API gate).
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Portal channel collab requires real daemon');
test.skip(!process.env.AEGIS_E2E_PORTAL_BROWSER, 'Invoked from verify-channel-portal-e2e.sh after API fan-out check');

const CHANNEL = process.env.AEGIS_PORTAL_CHANNEL || 'main';
const MARKER = process.env.AEGIS_PORTAL_E2E_MARKER || 'PORTAL-E2E-VERIFY';
const POST_MSG =
  process.env.AEGIS_PORTAL_E2E_MSG ||
  `${MARKER}: Can you all tell me one improvement you would make if you had a magic wand?`;

const EXPECTED_AGENTS = [
  'project-manager',
  'court-persona-ciso',
  'court-persona-security-architect',
  'court-persona-architect',
  'court-persona-senior-coder',
  'court-persona-tester',
  'court-persona-efficiency',
  'court-persona-user-advocate',
];

async function openChannels(page) {
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('nav-channels').click();
  await expect(page.locator('[data-testid="channels-panel"]:not([hidden])')).toBeVisible({ timeout: 10000 });
}

async function openMainChannel(page) {
  const item = page.locator('[data-testid="channels-list"]').getByText(CHANNEL, { exact: false }).first();
  await expect(item).toBeVisible({ timeout: 30000 });
  await item.click();
  await expect(page.locator('[data-testid="channel-detail"]')).toBeVisible({ timeout: 10000 });
}

test.describe('Portal channel collaboration (browser post + agent replies)', () => {
  test('Channels UI: post broadcast question and see agent replies', async ({ page, request }) => {
    // API path already validated in verify script; confirm marker visible in REST payload.
    const apiRes = await request.get(`/api/channels/${CHANNEL}`);
    expect(apiRes.ok()).toBeTruthy();
    const body = await apiRes.json();
    const messages = body.messages || [];
    const hasMarker = messages.some(
      (m) =>
        String(m.content || '').includes(MARKER) &&
        ['user', 'operator', 'web-portal', 'portal', 'cli'].includes(String(m.from || '').toLowerCase()),
    );
    expect(hasMarker).toBeTruthy();

    await openChannels(page);
    await openMainChannel(page);

    const messagesEl = page.locator('[data-testid="channel-messages"]');
    await expect(messagesEl).toBeVisible({ timeout: 10000 });
    await expect(messagesEl).toContainText(MARKER, { timeout: 15000 });

    for (const agent of EXPECTED_AGENTS) {
      const pattern =
        agent === 'project-manager'
          ? /project manager|coordinate|plan/i
          : new RegExp(agent.replace('court-persona-', '').replace(/-/g, '[- ]?'), 'i');
      await expect(messagesEl).toContainText(pattern, { timeout: 5000 });
    }

    // Post a follow-up through the real form (default from=user in SPA).
    const followUp = `${MARKER}-UI: Can you all share one quick tip for new users?`;
    await page.locator('#postContent').fill(followUp);
    await page.locator('#channelPostForm').evaluate((form) => form.requestSubmit());

    await expect(messagesEl).toContainText(`${MARKER}-UI`, { timeout: 20000 });
  });
});
