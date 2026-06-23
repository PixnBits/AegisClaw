import { test, expect } from '@playwright/test';

// Portal SPA REST contract tests (latency + JSON shape). Requires running daemon on :8080.
test.skip(!!process.env.AEGIS_E2E_FIXTURE, 'Portal API tests require real daemon');
test.skip(!process.env.AEGIS_E2E_PORTAL_API_BROWSER, 'Invoked from verify-portal-api-e2e.sh');

const MAX_MS = Number(process.env.AEGIS_PORTAL_API_MAX_MS || 30000);

const SPA_GETS = [
  { path: '/api/dashboard', fields: ['quick_stats', 'agents'] },
  { path: '/api/monitoring', fields: ['stats', 'agents', 'logs'] },
  { path: '/api/skills', array: true },
  { path: '/api/proposals', array: true },
  { path: '/api/channels', fields: ['channels'] },
];

test.describe('Portal SPA API contract (latency + shape)', () => {
  for (const spec of SPA_GETS) {
    test(`GET ${spec.path} < ${MAX_MS}ms`, async ({ request }) => {
      const start = Date.now();
      const res = await request.get(spec.path, { timeout: MAX_MS + 5000 });
      const elapsed = Date.now() - start;

      expect(res.status(), `${spec.path} should not 404/500`).toBe(200);
      expect(elapsed, `${spec.path} latency`).toBeLessThan(MAX_MS);

      const body = await res.json();
      if (spec.array) {
        expect(Array.isArray(body)).toBeTruthy();
      } else if (spec.fields) {
        for (const f of spec.fields) {
          expect(body).toHaveProperty(f);
        }
      }
    });
  }

  test('GET /api/channels/main returns messages array', async ({ request }) => {
    const res = await request.get('/api/channels/main', { timeout: MAX_MS + 5000 });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('messages');
    expect(Array.isArray(body.messages)).toBeTruthy();
  });

  test('GET /api/agents returns agent cards array', async ({ request }) => {
    const res = await request.get('/api/agents', { timeout: MAX_MS + 5000 });
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('agents');
    expect(Array.isArray(body.agents)).toBeTruthy();
    for (const agent of body.agents) {
      expect(agent).toHaveProperty('name');
      expect(agent).toHaveProperty('status');
    }
  });

  test('POST /api/channels/{id} accepts portal user post', async ({ request }) => {
    const marker = `PORTAL-API-E2E-${Date.now()}`;
    const chRes = await request.get('/api/channels/main');
    expect(chRes.ok()).toBeTruthy();

    const postRes = await request.post('/api/channels/main', {
      data: { from: 'user', content: `${marker}: portal API post smoke` },
      timeout: MAX_MS + 5000,
    });
    expect(postRes.ok()).toBeTruthy();

    const after = await request.get('/api/channels/main');
    const data = await after.json();
    const messages = data.messages || [];
    expect(messages.some((m) => String(m.content || '').includes(marker))).toBeTruthy();
  });

  test('STOMP /stomp delivers channel post notification', async ({ page }) => {
    const marker = `STOMP-PW-${Date.now()}`;
    const channelId = 'main';
    await page.goto('/');

    const gotMessage = await page.evaluate(
      async ({ marker, channelId, timeoutMs }) => {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(`${proto}//${location.host}/stomp`);
        let subscribed = false;

        return new Promise((resolve, reject) => {
          const timer = setTimeout(() => {
            ws.close();
            reject(new Error('STOMP MESSAGE timeout'));
          }, timeoutMs);

          ws.onopen = () => ws.send('CONNECT\naccept-version:1.2\n\n\x00');

          ws.onmessage = async (ev) => {
            const data = String(ev.data);
            if (data.startsWith('CONNECTED') && !subscribed) {
              subscribed = true;
              ws.send(
                `SUBSCRIBE\nid:sub-${channelId}\ndestination:/topic/channels.${channelId}.messages\n\n\x00`,
              );
              const res = await fetch(`/api/channels/${channelId}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ from: 'user', content: `${marker}: stomp playwright` }),
              });
              if (!res.ok) {
                clearTimeout(timer);
                reject(new Error(`post failed: ${res.status}`));
              }
              return;
            }
            if (data.startsWith('MESSAGE') && data.includes(marker)) {
              clearTimeout(timer);
              ws.close();
              resolve(true);
            }
          };

          ws.onerror = () => {
            clearTimeout(timer);
            reject(new Error('WebSocket error'));
          };
        });
      },
      { marker, channelId, timeoutMs: MAX_MS },
    );

    expect(gotMessage).toBeTruthy();
  });
});
