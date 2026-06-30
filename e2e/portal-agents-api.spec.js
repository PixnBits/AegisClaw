import { test, expect } from '@playwright/test';

// Regression: GET /api/agents must return {"agents":[...]} with at least one card when
// worker.list is populated (fixture seeds 3 workers; live daemon should expose PM/Court/coder).
const isFixture = !!process.env.AEGIS_E2E_FIXTURE;

test.describe('GET /api/agents regression', () => {
  test('returns wrapped non-empty agents array', async ({ request }) => {
    const res = await request.get('/api/agents', { timeout: 30_000 });
    expect(res.ok(), await res.text()).toBeTruthy();

    const body = await res.json();
    expect(body).not.toEqual([]);
    expect(body).toHaveProperty('agents');
    expect(Array.isArray(body.agents)).toBeTruthy();
    expect(body.agents.length, 'agents list must not be empty (worker.list / roster regression)').toBeGreaterThan(0);

    for (const agent of body.agents) {
      expect(agent).toHaveProperty('name');
      expect(agent).toHaveProperty('status');
      expect(String(agent.name || '')).not.toBe('');
    }

    const names = body.agents.map((a) => String(a.name || ''));
    if (isFixture) {
      expect(names.some((n) => n.includes('coder') || n.includes('researcher') || n.includes('builder'))).toBeTruthy();
    } else {
      // Live daemon: expect collaboration roster ids (PM, court persona, or coder).
      const hasRosterAgent = names.some(
        (n) =>
          n.includes('project-manager') ||
          n.startsWith('court-persona-') ||
          n.includes('coder') ||
          n.startsWith('agent-'),
      );
      expect(hasRosterAgent, `expected roster agent in ${JSON.stringify(names)}`).toBeTruthy();
    }
  });

  test('GET /api/llm-usage returns aggregate shape (Phase 1 metrics)', async ({ request }) => {
    // Contract / fixture test: always validates response shape (even with zero data in fixture mode).
    // Meaningful data (non-zero after real LLM on active channel) is asserted in collaboration.spec.js
    // (which runs only in real daemon + make test-e2e-llm after CLI pm goal).
    const res = await request.get('/api/llm-usage', { timeout: 10_000 });
    expect(res.ok(), await res.text()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('grand');
    expect(body).toHaveProperty('last_hour');
    expect(body).toHaveProperty('today');
    expect(body).toHaveProperty('mtd');
  });
});