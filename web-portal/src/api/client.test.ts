import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from './client';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('fetchJSON errors', () => {
  it('surfaces the server response text', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= 45 chars\n', { status: 400 })),
    );
    await expect(api.createChannel('MyProj')).rejects.toThrow(
      'invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= 45 chars',
    );
  });

  it('falls back to the status when the body is empty', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('', { status: 503, statusText: 'Service Unavailable' })));
    await expect(api.goals('ship')).rejects.toThrow(/503/);
  });

  it('surfaces a 502 body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('goal.submit: ensure PM for main: invalid vm id: refused\n', { status: 502 })),
    );
    await expect(api.goals('ship', 'main')).rejects.toThrow('invalid vm id: refused');
  });
});
