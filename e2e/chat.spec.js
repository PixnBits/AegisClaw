import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const LOOP_STEP_RE = /Starting|Observe|Think|Plan|Act|Execute|Judge/i;
const AGENT_STEP_PHASES = ['Starting', 'Observe', 'Think', 'Plan', 'Act', 'Execute', 'Judge'];

/** Session IDs with a running paired agent VM (from `aegis status`). */
function runningAgentSessionIds() {
  try {
    const out = execSync('./bin/aegis status', { cwd: process.cwd(), encoding: 'utf8', timeout: 15_000 });
    const ids = new Set();
    for (const m of out.matchAll(/agent-([a-z0-9]+)/g)) {
      ids.add(m[1]);
    }
    return [...ids];
  } catch {
    return [];
  }
}

/** Pick or create a session whose paired agent VM is running. */
async function activateWarmedSession(page) {
  const warmed = runningAgentSessionIds();
  const listRes = await page.request.get('/api/chat/sessions');
  expect(listRes.ok()).toBeTruthy();
  const listBody = await listRes.json();
  const sessions = Array.isArray(listBody.sessions) ? listBody.sessions : [];

  let target = sessions.find((s) => warmed.includes(s.id));
  if (!target && sessions.length > 0) {
    target = sessions[0];
  }
  if (!target) {
    const createRes = await page.request.post('/api/chat/sessions', {
      data: { title: 'E2E progress session' },
    });
    expect(createRes.ok()).toBeTruthy();
    const created = await createRes.json();
    target = created.session;
    expect(target?.id).toBeTruthy();
    await page.reload();
    await expect(page.getByTestId('chat-input')).toBeVisible({ timeout: 15_000 });
  }

  const historyLoaded = page.waitForResponse(
    (res) =>
      res.url().includes('/api/chat/history') &&
      res.url().includes(encodeURIComponent(target.id)) &&
      res.ok(),
    { timeout: 30_000 },
  );
  await page.locator(`[data-session-id="${target.id}"]`).first().click();
  await historyLoaded;
  await expect(page.getByTestId('chat-input')).toBeEnabled({ timeout: 15_000 });

  return target.id;
}

/** Inject a fetch hook so SSE frames are captured in-page without blocking on stream close. */
async function installChatStreamCapture(page) {
  await page.addInitScript(() => {
    window.__aegisChatSse = [];
    const orig = window.fetch;
    window.fetch = async function (...args) {
      const res = await orig.apply(this, args);
      const url = typeof args[0] === 'string' ? args[0] : args[0]?.url || '';
      if (!url.includes('/chat/send')) {
        return res;
      }
      const ctype = (res.headers.get('content-type') || '').toLowerCase();
      if (!ctype.includes('text/event-stream') || !res.body) {
        return res;
      }
      const [live, tap] = res.body.tee();
      (async () => {
        const reader = tap.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          let cut = buf.indexOf('\n\n');
          while (cut >= 0) {
            const frame = buf.slice(0, cut);
            buf = buf.slice(cut + 2);
            const line = frame.split('\n').find((l) => l.startsWith('data:'));
            if (line) {
              try {
                window.__aegisChatSse.push(JSON.parse(line.slice(5).trim()));
              } catch {
                /* ignore */
              }
            }
            cut = buf.indexOf('\n\n');
          }
        }
      })();
      return new Response(live, { status: res.status, statusText: res.statusText, headers: res.headers });
    };
  });
}

async function readCapturedSseFrames(page) {
  return page.evaluate(() => window.__aegisChatSse || []);
}

/**
 * Real-system chat E2E — requires the full stack (`make start`).
 * Exercises the dashboard /chat page served by the daemon reverse proxy.
 */
test.describe('Chat (real system)', () => {
  test.describe.configure({ mode: 'serial', timeout: 300_000 });

  test('loads the chat page', async ({ page }) => {
    const sessionsLoaded = page.waitForResponse(
      (res) => res.url().includes('/api/chat/sessions') && res.request().method() === 'GET' && res.ok(),
      { timeout: 30_000 },
    );
    await page.goto('/chat');
    await expect(page).toHaveTitle(/Chat.*AegisClaw/);
    await expect(page.getByTestId('chat-input')).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId('chat-send-button')).toBeEnabled();
    await sessionsLoaded;
  });

  test('shows 6-step agent loop progress while responding', async ({ page }) => {
    await installChatStreamCapture(page);
    const sessionsLoaded = page.waitForResponse(
      (res) => res.url().includes('/api/chat/sessions') && res.request().method() === 'GET' && res.ok(),
      { timeout: 30_000 },
    );
    await page.goto('/chat');
    await expect(page.getByTestId('chat-input')).toBeVisible({ timeout: 15_000 });
    await sessionsLoaded;

    await activateWarmedSession(page);

    const userMessage = `Hello ther! ${Date.now()}`;

    await page.getByTestId('chat-input').fill(userMessage);
    await page.getByTestId('chat-input').press('Enter');

    const messages = page.getByTestId('chat-messages');
    await expect(messages.locator('.msg-user .bubble').last()).toContainText(userMessage, {
      timeout: 15_000,
    });

    const progressLog = messages.locator('[data-testid="chat-progress-log"]');
    await expect(progressLog).toBeVisible({ timeout: 30_000 });

    const thoughtSteps = messages.locator('[data-testid="thought-step"]');
    await expect(thoughtSteps.first()).toBeVisible({ timeout: 120_000 });
    await expect(progressLog).toContainText(LOOP_STEP_RE, { timeout: 120_000 });

    const visiblePhases = await thoughtSteps.locator('.thought-phase, .thought-phase--thinking').allTextContents();
    const matched = AGENT_STEP_PHASES.filter((label) =>
      visiblePhases.some((text) => text.toLowerCase().includes(label.toLowerCase())),
    );
    expect(matched.length).toBeGreaterThanOrEqual(3);

    const assistantBubble = messages.locator('.msg-assistant .bubble').last();
    await expect(assistantBubble).toBeVisible({ timeout: 180_000 });
    await expect(assistantBubble).not.toContainText('Network error');
    await expect(assistantBubble).not.toContainText('agent VM may still be starting');

    await expect(messages.locator('.thought-log').filter({ hasText: 'Thinking trace' })).toBeVisible({
      timeout: 30_000,
    });
    await expect(messages.locator('.thought-log').filter({ hasText: 'Thinking trace' })).toContainText(
      LOOP_STEP_RE,
    );

    const sseFrames = await readCapturedSseFrames(page);
    const sawProgressEvent = sseFrames.some(
      (ev) =>
        (ev.type === 'thought_event' && ev.event) ||
        (ev.type === 'thought_delta' && LOOP_STEP_RE.test(String(ev.delta || ''))),
    );
    expect(sawProgressEvent).toBeTruthy();
  });

  test('send a message and receive an assistant reply', async ({ page }) => {
    const sessionsLoaded = page.waitForResponse(
      (res) => res.url().includes('/api/chat/sessions') && res.request().method() === 'GET' && res.ok(),
      { timeout: 30_000 },
    );
    await page.goto('/chat');
    await expect(page.getByTestId('chat-input')).toBeVisible({ timeout: 15_000 });
    await sessionsLoaded;

    await activateWarmedSession(page);

    const userMessage = `E2E chat ping ${Date.now()}`;
    await page.getByTestId('chat-input').fill(userMessage);
    await page.getByTestId('chat-input').press('Enter');

    const messages = page.getByTestId('chat-messages');
    await expect(messages.locator('.msg-user .bubble').last()).toContainText(userMessage, {
      timeout: 10_000,
    });

    await expect(messages.locator('.msg-typing, .msg-assistant .bubble').first()).toBeVisible({
      timeout: 15_000,
    });

    const assistantBubble = messages.locator('.msg-assistant .bubble').last();
    await expect(assistantBubble).toBeVisible({ timeout: 180_000 });
    await expect(assistantBubble).not.toHaveText('');
    await expect(assistantBubble).not.toContainText('Network error');
    await expect(assistantBubble).not.toContainText('Error:');
  });

  test('shows typing feedback quickly after send', async ({ page }) => {
    const sessionsLoaded = page.waitForResponse(
      (res) => res.url().includes('/api/chat/sessions') && res.request().method() === 'GET' && res.ok(),
      { timeout: 30_000 },
    );
    await page.goto('/chat');
    await expect(page.getByTestId('chat-input')).toBeVisible({ timeout: 15_000 });
    await sessionsLoaded;

    await activateWarmedSession(page);

    const started = Date.now();
    await page.getByTestId('chat-input').fill(`E2E stream timing ${Date.now()}`);
    await page.getByTestId('chat-input').press('Enter');

    const messages = page.getByTestId('chat-messages');
    await expect(messages.locator('.msg-typing').first()).toBeVisible({ timeout: 15_000 });
    expect(Date.now() - started).toBeLessThan(15_000);

    await expect(messages.locator('.msg-assistant .bubble').last()).toBeVisible({ timeout: 120_000 });
  });

  test('ignores empty messages', async ({ page }) => {
    await page.goto('/chat');
    await expect(page.getByTestId('chat-input')).toBeVisible({ timeout: 15_000 });

    const before = await page.getByTestId('chat-messages').locator('.msg-user').count();
    await page.getByTestId('chat-send-button').click();
    await expect(page.getByTestId('chat-messages').locator('.msg-user')).toHaveCount(before);
  });
});
