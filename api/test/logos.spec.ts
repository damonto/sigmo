import { env } from 'cloudflare:workers';
import {
  createExecutionContext,
  waitOnExecutionContext,
} from 'cloudflare:test';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import worker from '../src/index';
import {
  CARRIER_LOGO_CATALOG_KEY,
  CARRIER_LOGO_CRON,
  SIMDEX_ORIGIN,
  SIMDEX_INDEX_URL,
} from '../src/logos/catalog';
import { carrierLogoCacheKey } from '../src/logos/service';

interface IconResponse {
  body: ArrayBuffer | string;
  contentType: string;
  status?: number;
}

const PNG_FIXTURE = Uint8Array.from(
  atob(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC',
  ),
  (character) => character.charCodeAt(0),
).buffer;
const SVG_FIXTURE =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="24" height="24" fill="#e20074"/></svg>';
const CACHE_ICON_PATHS = [
  '/icons/test/lazy.png',
  '/icons/test/shared.svg?v=test1',
  '/icons/test/text.svg',
  '/icons/test/text-tspan.svg',
  '/icons/test/text-path.svg',
  '/icons/test/direct.png',
  '/icons/test/missing.png',
  '/icons/test/reload-a.png',
  '/icons/test/reload-b.png',
];

async function clearLogoState(): Promise<void> {
  await env.BUCKET.delete(CARRIER_LOGO_CATALOG_KEY);
  for (const icon of CACHE_ICON_PATHS) {
    await caches.default.delete(carrierLogoCacheKey(icon));
  }
}

afterEach(() => vi.restoreAllMocks());
beforeEach(clearLogoState);

function sourceIndex(
  carriers: Array<{ icon: string | null; plmns: string[] }>,
): Record<string, unknown> {
  const fillers = Array.from({ length: 110 }, (_, index) => ({
    icon: `/icons/test/filler-${index}.png`,
    plmns: [String(50_000 + index)],
  }));
  return {
    generatedAt: '2026-10-22',
    total: carriers.length + fillers.length,
    warnings: [],
    carriers: [...carriers, ...fillers],
  };
}

function mockUpstream(
  index: unknown,
  icons: Record<string, IconResponse> = {},
): string[] {
  const calls: string[] = [];
  vi.spyOn(globalThis, 'fetch').mockImplementation(
    async (input, init): Promise<Response> => {
      const request = new Request(input, init);
      const url = new URL(request.url);
      calls.push(`${url.pathname}${url.search}`);
      if (url.href === SIMDEX_INDEX_URL) {
        return Response.json(index, {
          headers: { etag: '"simdex-test"' },
        });
      }
      const item = icons[url.pathname];
      if (!item) {
        return new Response(null, { status: 404 });
      }
      return new Response(item.body, {
        status: item.status ?? 200,
        headers: { 'content-type': item.contentType },
      });
    },
  );
  return calls;
}

async function seedCatalog(logos: Record<string, string>): Promise<void> {
  await env.BUCKET.put(
    CARRIER_LOGO_CATALOG_KEY,
    JSON.stringify({
      schemaVersion: 1,
      generatedAt: '2026-10-22',
      syncedAt: '2026-10-22T03:17:00.000Z',
      logos,
    }),
    {
      httpMetadata: { contentType: 'application/json' },
    },
  );
}

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

async function runScheduled(cron: string): Promise<void> {
  await worker.scheduled(
    {
      scheduledTime: Date.now(),
      cron,
      noRetry() {},
    },
    env,
  );
}

describe('carrier logo catalog', () => {
  it('keeps the first PLMN record and ignores invalid paths', async () => {
    const calls = mockUpstream(
      sourceIndex([
        {
          icon: '/icons/carriers/First.svg?v=test1',
          plmns: ['310070'],
        },
        { icon: '/icons/test/second.png', plmns: ['310070'] },
        { icon: null, plmns: ['20433'] },
        { icon: '/icons/test/late.svg', plmns: ['20433'] },
        { icon: '/icons/../escape.png', plmns: ['310120'] },
        { icon: 'https://example.com/icon.svg', plmns: ['310130'] },
        { icon: '/icons/test/fragment.svg#icon', plmns: ['310131'] },
        { icon: '/icons/test/ok.svg', plmns: ['310-070'] },
      ]),
    );

    await runScheduled(CARRIER_LOGO_CRON);

    const object = await env.BUCKET.get(CARRIER_LOGO_CATALOG_KEY);
    if (!object) {
      throw new Error('missing carrier logo catalog');
    }
    const catalog = await object.json<{
      generatedAt: string;
      logos: Record<string, string>;
      schemaVersion: number;
    }>();
    expect(catalog.schemaVersion).toBe(1);
    expect(catalog.generatedAt).toBe('2026-10-22');
    expect(catalog.logos['310070']).toBe('/icons/carriers/First.svg?v=test1');
    expect(catalog.logos['20433']).toBeUndefined();
    expect(catalog.logos['310120']).toBeUndefined();
    expect(catalog.logos['310130']).toBeUndefined();
    expect(catalog.logos['310131']).toBeUndefined();
    expect(catalog.logos['310-070']).toBeUndefined();
    expect(calls).toEqual(['/data/index.json']);
  });

  it('keeps the previous catalog when the source is incomplete', async () => {
    await seedCatalog({ 310070: '/icons/test/previous.png' });
    const previous = await env.BUCKET.get(CARRIER_LOGO_CATALOG_KEY);
    if (!previous) {
      throw new Error('missing previous carrier logo catalog');
    }
    const previousBody = await previous.text();
    mockUpstream({
      generatedAt: '2026-10-23',
      carriers: [{ icon: null, plmns: ['310070'] }],
    });
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});

    await expect(runScheduled(CARRIER_LOGO_CRON)).rejects.toThrow(
      'incomplete simdex carrier index',
    );

    const current = await env.BUCKET.get(CARRIER_LOGO_CATALOG_KEY);
    expect(await current?.text()).toBe(previousBody);
    expect(error).toHaveBeenCalled();
  });

  it('bootstraps the catalog on the first valid logo request', async () => {
    const calls = mockUpstream(
      sourceIndex([{ icon: '/icons/test/lazy.png', plmns: ['310070'] }]),
      {
        '/icons/test/lazy.png': {
          body: PNG_FIXTURE,
          contentType: 'image/png',
        },
      },
    );

    const response = await send('310070');

    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('image/png');
    expect(calls).toEqual(['/data/index.json', '/icons/test/lazy.png']);
    expect(await env.BUCKET.get(CARRIER_LOGO_CATALOG_KEY)).not.toBeNull();
  });
});

describe('carrier logo proxy', () => {
  it('observes catalog changes without retaining request state', async () => {
    const calls = mockUpstream(undefined, {
      '/icons/test/reload-a.png': {
        body: PNG_FIXTURE,
        contentType: 'image/png',
      },
      '/icons/test/reload-b.png': {
        body: PNG_FIXTURE,
        contentType: 'image/png',
      },
    });
    await seedCatalog({ 310070: '/icons/test/reload-a.png' });
    expect((await send('310070')).status).toBe(200);

    await seedCatalog({ 310070: '/icons/test/reload-b.png' });
    expect((await send('310070')).status).toBe(200);
    expect(calls).toEqual([
      '/icons/test/reload-a.png',
      '/icons/test/reload-b.png',
    ]);
  });

  it('rasterizes one shared SVG and reuses its cached PNG', async () => {
    await seedCatalog({
      310070: '/icons/test/shared.svg?v=test1',
      310410: '/icons/test/shared.svg?v=test1',
    });
    const calls = mockUpstream(undefined, {
      '/icons/test/shared.svg': {
        body: SVG_FIXTURE,
        contentType: 'image/svg+xml; charset=utf-8',
      },
    });

    const first = await send('310070');
    const firstBytes = new Uint8Array(await first.arrayBuffer());
    expect(first.status).toBe(200);
    expect(first.headers.get('content-type')).toBe('image/png');
    expect(first.headers.get('cache-control')).toBe('public, max-age=86400');
    expect([...firstBytes.slice(0, 8)]).toEqual([
      0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
    ]);

    const second = await send('310410');
    expect(second.status).toBe(200);
    expect(await second.arrayBuffer()).toEqual(firstBytes.buffer);
    expect(calls).toEqual(['/icons/test/shared.svg?v=test1']);
    expect(carrierLogoCacheKey('/icons/test/shared.svg?v=test1').url).toBe(
      `${SIMDEX_ORIGIN}/icons/test/shared.svg?v=test1&sigmo-png=v1`,
    );
  });

  it.each([
    ['<text>', '/icons/test/text.svg', '<text x="12" y="12">A</text>'],
    ['<tspan>', '/icons/test/text-tspan.svg', '<tspan x="12" y="12">A</tspan>'],
    [
      '<textPath>',
      '/icons/test/text-path.svg',
      '<textPath href="#path">A</textPath>',
    ],
  ])('discards an SVG containing %s', async (_, icon, textMarkup) => {
    await seedCatalog({ 310070: icon });
    const calls = mockUpstream(undefined, {
      [icon]: {
        body: SVG_FIXTURE.replace('</svg>', `${textMarkup}</svg>`),
        contentType: 'image/svg+xml',
      },
    });

    const response = await send('310070');
    const cached = await send('310070');

    expect(response.status).toBe(404);
    expect(cached.status).toBe(404);
    expect(response.headers.get('cache-control')).toBe('public, max-age=300');
    expect(calls).toEqual([icon]);
    const negativeCache = await caches.default.match(carrierLogoCacheKey(icon));
    expect(negativeCache?.status).toBe(404);
  });

  it('serves PNG sources with HEAD and conditional GET', async () => {
    await seedCatalog({ 310070: '/icons/test/direct.png' });
    const calls = mockUpstream(undefined, {
      '/icons/test/direct.png': {
        body: PNG_FIXTURE,
        contentType: 'image/png',
      },
    });
    const first = await send('310070');
    expect(first.status).toBe(200);
    expect(new Uint8Array(await first.arrayBuffer())).toEqual(
      new Uint8Array(PNG_FIXTURE),
    );
    const tag = first.headers.get('etag');
    expect(tag).toBeTruthy();

    const head = await send('310070', { method: 'HEAD' });
    expect(head.status).toBe(200);
    expect(await head.text()).toBe('');
    expect(head.headers.get('content-length')).toBe(
      String(PNG_FIXTURE.byteLength),
    );

    for (const validator of [String(tag), `W/${tag}`, `"other", ${tag}`, '*']) {
      const cached = await send('310070', {
        headers: { 'if-none-match': validator },
      });
      expect(cached.status).toBe(304);
      expect(await cached.text()).toBe('');
    }
    const changed = await send('310070', {
      headers: { 'if-none-match': '"old"' },
    });
    expect(changed.status).toBe(200);
    expect(calls).toEqual(['/icons/test/direct.png']);
  });

  it.each(['26606', '99999', '310-070', '3107', 'logo.svg'])(
    'returns a short-lived 404 for %s',
    async (plmn) => {
      await seedCatalog({ 310070: '/icons/test/missing.png' });
      const upstream = vi.spyOn(globalThis, 'fetch');

      const response = await send(plmn);

      expect(response.status).toBe(404);
      expect(response.headers.get('cache-control')).toBe('public, max-age=300');
      expect(upstream).not.toHaveBeenCalled();
    },
  );

  it('returns 404 when the upstream icon is missing', async () => {
    await seedCatalog({ 310070: '/icons/test/missing.png' });
    const calls = mockUpstream(undefined, {
      '/icons/test/missing.png': {
        body: '',
        contentType: 'text/plain',
        status: 404,
      },
    });

    const response = await send('310070');
    const cached = await send('310070');

    expect(response.status).toBe(404);
    expect(cached.status).toBe(404);
    expect(response.headers.get('cache-control')).toBe('public, max-age=300');
    expect(calls).toEqual(['/icons/test/missing.png']);
  });

  it('does not cache catalog failures', async () => {
    vi.spyOn(env.BUCKET, 'get').mockRejectedValue(new Error('storage offline'));

    const response = await send('310070');

    expect(response.status).toBe(503);
    expect(response.headers.get('cache-control')).toBe('no-store');
  });

  it('derives OPTIONS and rejects writes', async () => {
    await seedCatalog({ 310070: '/icons/test/direct.png' });
    mockUpstream(undefined, {
      '/icons/test/direct.png': { body: PNG_FIXTURE, contentType: 'image/png' },
    });

    expect(
      (await send('310070', { method: 'OPTIONS' })).headers.get('allow'),
    ).toContain('HEAD');
    expect((await send('310070', { method: 'POST' })).status).toBe(405);
  });
});
