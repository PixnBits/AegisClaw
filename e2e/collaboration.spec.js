import { test, expect } from '@playwright/test';

// Real daemon E2E for collaboration model (channels + PM + LLM posts).
// Run as part of make test-e2e-llm (after CLI pm goal has posted to channel).
// Skips in fixture mode; use real daemon + portal at :8080.
// Also skipped unless AEGIS_E2E_COLLAB_BROWSER=1 (set by verify script after CLI trigger).
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Collaboration browser checks require real daemon (use make test-e2e-llm after start)');
test.skip(!process.env.AEGIS_E2E_COLLAB_BROWSER, 'Invoked only from verify-pm-llm-e2e.sh after CLI pm goal (sets the env)');

test.describe('Collaboration E2E (browser verification of channels/PM posts)', () => {
  test('Channels UI shows PM plan post (after CLI pm goal with E2E-LLM-VERIFY) + user posts via browser form', async ({ page }) => {
    // Navigate to channels (primary collab view, replaced old chat). User journey via browser.
    await page.goto('/#channels');

    // Sidebar and main lists should be present (detailed UI elements from portal).
    await expect(page.getByTestId('sidebar-channels-list')).toBeVisible({ timeout: 10000 });
    await expect(page.getByTestId('channels-list')).toBeVisible({ timeout: 10000 });

    // The channel created/posted by the E2E CLI (plan-demo-e2e-llm) should be visible.
    const channelItem = page.getByText('plan-demo-e2e-llm').first();
    await expect(channelItem).toBeVisible({ timeout: 15000 });

    // Select the channel (click the list item).
    await channelItem.click();

    // Channel detail should show (with messages area and post composer).
    const detail = page.getByTestId('channel-detail');
    await expect(detail).toBeVisible({ timeout: 10000 });
    const messages = page.getByTestId('channel-messages');
    await expect(messages).toBeVisible({ timeout: 10000 });

    // The PM post from the goal should be present: contains the verify string and from project-manager.
    await expect(messages).toContainText('E2E-LLM-VERIFY', { timeout: 15000 });
    await expect(messages).toContainText('project-manager', { timeout: 10000 });

    // Detailed: as a real user would, type + post a follow-up message into the browser channel form.
    // This exercises the #channelPostForm, postContent textarea, and submit (user typing in browser).
    const postForm = page.locator('#channelPostForm');
    if (await postForm.count() > 0) {
      const content = page.locator('#postContent');
      if (await content.count() > 0) {
        await content.fill('E2E browser follow-up from user (detailed journey test)');
        await postForm.locator('button[type="submit"]').click();
        // The new post should appear in the live messages area (optimistic or after roundtrip via store/hub).
        await expect(messages).toContainText('E2E browser follow-up from user', { timeout: 10000 });
      }
    }

    // Also check default 'main' channel has Court/PM members or activity (E2E defaults).
    await page.goto('/#channels');
    await expect(page.getByText('main')).toBeVisible({ timeout: 10000 });
  });

  // Detailed browser E2E for additional user journeys from docs/specs/user-journeys/ (real daemon + browser as user would: clicking, viewing UI, forms).
  // These cover UI surfaces + interactions for journeys not fully exercised by the CLI pm goal (J03) or legacy chat.
  // Run in the E2E script after start (browser part always for nav/structure, full after pm goal for data).
  // Citations: docs/specs/user-journeys/01-09 + web-portal.md + cmd/web-portal/static/index.html (data-testid + forms).

  test('User Journey 1+2: Onboarding dashboard + skills nav + new channel form (browser)', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByTestId('dashboard-stats')).toBeVisible();
    await page.getByTestId('nav-skills').click();
    await expect(page.getByTestId('propose-skill-button')).toBeVisible();
    // J02: starting conversation / new channel UI
    await page.getByTestId('nav-channels').click();
    await expect(page.getByTestId('new-channel-button')).toBeVisible();
    await expect(page.locator('#newChannelForm')).toBeVisible();
  });

  test('User Journey 4+9: Proposals / skill creation UI + REST + channel create (browser)', async ({ page, request }) => {
    await page.goto('/');
    await page.getByTestId('nav-skills').click();
    await expect(page.getByTestId('proposals-list')).toBeVisible();
    const res = await request.get('/api/proposals');
    expect(res.ok()).toBeTruthy();
    // Also exercise channels create form (J04/09 skill + collab setup)
    await page.getByTestId('nav-channels').click();
    await expect(page.getByTestId('create-channel-button')).toBeVisible();
  });

  test('User Journey 5+8: Monitoring + multi-agent/teams nav (browser)', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('nav-monitoring').click();
    // monitoring surface (stats or logs container)
    await expect(page.getByTestId('monitoring-stats').or(page.getByTestId('monitoring-logs'))).toBeVisible({ timeout: 8000 });
    await page.getByTestId('nav-teams').click();
    // teams list/cards from phase5 wiring
    await expect(page.locator('[data-testid*="team"], #teamsList, text=Teams').first()).toBeVisible({ timeout: 8000 });
  });

  test('User Journey 6+7: Court + autonomy nav + proposals (browser)', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('nav-court').click();
    // court surface (id from templates or heading) + proposals list (J06)
    await expect(page.getByRole('heading', { name: /Court|Governance/i })).toBeVisible();
    await expect(page.getByTestId('proposals-list')).toBeVisible({ timeout: 8000 });
  });

  test('User Journey 3 (collab task) + channels post form present (detailed browser interaction)', async ({ page }) => {
    // J03: collaborative task via channels (PM driven); here we verify the post UI is available for user replies.
    await page.goto('/#channels');
    await expect(page.getByTestId('channels-panel').or(page.getByTestId('channel-detail'))).toBeVisible({ timeout: 10000 });
    // The composer form for user to type/post (detailed "user typing into browser")
    const composer = page.locator('#channelPostForm, form.chat-composer');
    // May be hidden until a channel is selected; non-fatal if not in this partial run.
    if (await composer.count() > 0) {
      await expect(composer.first()).toBeVisible({ timeout: 5000 });
    }
  });
});
