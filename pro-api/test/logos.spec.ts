import { env } from 'cloudflare:workers';
import {
  createExecutionContext,
  waitOnExecutionContext,
} from 'cloudflare:test';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import worker from '../src/index';
import catalog from '../src/logos/carriers.json';

afterEach(() => vi.restoreAllMocks());
beforeEach(async () => {
  await env.BUCKET.delete('logos/att.com.png');
});

async function send(path: string, init: RequestInit = {}): Promise<Response> {
  const context = createExecutionContext();
  const response = await worker.fetch(
    new Request(`https://logos.example/logo/${path}`, init),
    env,
    context,
  );
  await waitOnExecutionContext(context);
  return response;
}

describe('carrier logos', () => {
  it('keeps catalog filenames inside logos/ and PLMNs unique', () => {
    const plmns = new Set<string>();
    for (const carrier of catalog) {
      expect(carrier.logo).toMatch(/^[a-z0-9][a-z0-9.-]*\.png$/);
      expect(carrier.plmns.length).toBeGreaterThan(0);
      for (const plmn of carrier.plmns) {
        expect(plmn).toMatch(/^[0-9]{5,6}$/);
        expect(plmns.has(plmn), `duplicate PLMN ${plmn}`).toBe(false);
        plmns.add(plmn);
      }
    }
  });

  it('serves imported direct images while retaining existing domain mappings', async () => {
    const carrier = catalog.find((entry) => entry.plmns.includes('53024'));
    if (!carrier) {
      throw new Error('missing imported 2degrees image');
    }
    await env.BUCKET.put(`logos/${carrier.logo}`, 'direct PNG fixture');
    try {
      const response = await send('53024');
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toBe('image/png');
      expect(await response.text()).toBe('direct PNG fixture');
      const retained = catalog.find((entry) => entry.plmns.includes('310070'));
      expect(retained?.logo).toBe('att.com.png');
    } finally {
      await env.BUCKET.delete(`logos/${carrier.logo}`);
    }
  });

  it('serves one PNG for multiple PLMNs without license or database access', async () => {
    const query = vi.spyOn(env.DB, 'prepare');
    const upstream = vi.spyOn(globalThis, 'fetch');
    await env.BUCKET.put('logos/att.com.png', 'png fixture');
    const first = await send('310070');
    expect(first.status).toBe(200);
    expect(first.headers.get('content-type')).toBe('image/png');
    expect(first.headers.get('cache-control')).toBe('public, max-age=86400');
    expect(await first.text()).toBe('png fixture');
    expect(await (await send('310410')).text()).toBe('png fixture');
    expect(query).not.toHaveBeenCalled();
    expect(upstream).not.toHaveBeenCalled();
  });

  it('supports HEAD, conditional GET and weak validators', async () => {
    await env.BUCKET.put('logos/att.com.png', 'png fixture');
    const read = vi.spyOn(env.BUCKET, 'get');
    const head = await send('310070', { method: 'HEAD' });
    expect(head.status).toBe(200);
    expect(await head.text()).toBe('');
    expect(head.headers.get('content-length')).toBe('11');
    expect(read).not.toHaveBeenCalled();
    const tag = head.headers.get('etag');
    expect(tag).toBeTruthy();
    for (const validator of [String(tag), `W/${tag}`, `"other", ${tag}`, '*']) {
      const cached = await send('310070', {
        headers: { 'if-none-match': validator },
      });
      expect(cached.status).toBe(304);
      expect(await cached.text()).toBe('');
      expect(await read.mock.results.at(-1)?.value).not.toHaveProperty('body');
      const cachedHead = await send('310070', {
        method: 'HEAD',
        headers: { 'if-none-match': validator },
      });
      expect(cachedHead.status).toBe(304);
      expect(await cachedHead.text()).toBe('');
    }
    const changed = await send('310070', {
      headers: { 'if-none-match': '"old"' },
    });
    expect(changed.status).toBe(200);
    expect(await changed.text()).toBe('png fixture');
  });

  it.each(['26606', '99999', '310-070', 'att.com.png', '310070'])(
    'returns a short-lived 404 for %s',
    async (plmn) => {
      const response = await send(plmn);
      expect(response.status).toBe(404);
      expect(response.headers.get('cache-control')).toBe('public, max-age=300');
    },
  );

  it('does not cache storage failures', async () => {
    vi.spyOn(env.BUCKET, 'get').mockRejectedValue(new Error('storage offline'));
    const response = await send('310070');
    expect(response.status).toBe(503);
    expect(response.headers.get('cache-control')).toBe('no-store');
  });

  it('derives OPTIONS and rejects writes', async () => {
    expect(
      (await send('310070', { method: 'OPTIONS' })).headers.get('allow'),
    ).toContain('HEAD');
    expect((await send('310070', { method: 'POST' })).status).toBe(405);
  });
});
