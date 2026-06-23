import { test, expect } from '@playwright/test';

// Driving E2E for turn-based message propagation per docs/specs/turn-based-message-propagation.md:
//   PM posts plan (assign @coder, flag @ciso) ->
//   Coder posts progress ->
//   CISO receives batched turn with relevance anchors, uses context, posts push-back.
//
// This is primarily driven by scripts/verify-turn-based-propagation-e2e.sh (CLI pm goal + polls + turn-state).
// The browser side is optional / smoke; enable via AEGIS_TURN_PROP_BROWSER=1 after the CLI phase
// has populated the channel (similar to collaboration.spec.js + verify-pm-llm-e2e).
//
// Skips unless a real daemon is present (no fixture mode) and the marker env is set.
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Requires real daemon + turn propagation scenario');
test.skip(!process.env.AEGIS_TURN_PROP_BROWSER && !process.env.AEGIS_E2E_COLLAB_BROWSER,
  'Set AEGIS_TURN_PROP_BROWSER=1 (or AEGIS_E2E_COLLAB_BROWSER) after running the turn verify script');

const CHANNEL = process.env.TURN_E2E_CHANNEL || 'turn-e2e-verify';

async function waitPortalReady(page) {
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-portal-ready="1"]', { timeout: 15000 });
}

async function openChannels(page) {
  await waitPortalReady(page);
  const nav = page.getByTestId('nav-channels');
  if ((await nav.count()) === 0) return false;
  await nav.click();
  await expect(page.locator('[data-testid="channels-panel"]:not([hidden])')).toBeVisible({ timeout: 10000 });
  return true;
}

test.describe('Turn-based propagation driving scenario (browser visibility)', () => {
  test('Channel shows PM plan, coder update, and CISO push-back with turn context', async ({ page, request }) => {
    // Ensure channel exists via API first (store is ready).
    await expect.poll(async () => {
      const r = await request.get(`/api/channels/${CHANNEL}`);
      return r.ok();
    }, { timeout: 30000 }).toBeTruthy();

    const api = await request.get(`/api/channels/${CHANNEL}`);
    expect(api.ok()).toBeTruthy();
    const body = await api.json();
    const msgs = body.messages || [];

    // PM plan assigned work and flagged CISO.
    const pmPlan = msgs.find(m =>
      /project-manager/i.test(String(m.from || '')) &&
      /plan|assign|coder/i.test(String(m.content || ''))
    );
    expect(pmPlan, 'PM plan with assignment visible via API').toBeTruthy();

    // Coder posted progress.
    const coderPost = msgs.find(m =>
      /coder/i.test(String(m.from || '')) &&
      String(m.content || '').trim().length > 0
    );
    expect(coderPost, 'coder progress visible').toBeTruthy();

    // CISO (court-persona-ciso) posted something relevant (push-back / review / concern).
    const cisoPost = msgs.find(m =>
      /ciso/i.test(String(m.from || '')) &&
      /(security|concern|push|review|risk|back)/i.test(String(m.content || ''))
    );
    expect(cisoPost, 'CISO response to batch with anchors visible').toBeTruthy();

    // Optional browser UI check (best effort).
    const uiReady = await openChannels(page);
    if (!uiReady) return;

    await expect(page.getByTestId('channels-list').getByText(CHANNEL, { exact: false }).first())
      .toBeVisible({ timeout: 15000 });

    // If the channel item can be clicked in this harness, messages should reflect the flow.
    // We already asserted via API; UI is supplementary.
  });
});
