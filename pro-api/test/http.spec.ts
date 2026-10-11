import { env } from 'cloudflare:workers';
import {
  createExecutionContext,
  waitOnExecutionContext,
} from 'cloudflare:test';
import { HTTPException } from 'hono/http-exception';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { Database } from '../src/storage/authorization';
import { PROBLEM_MEDIA_TYPE, PROBLEM_TYPE_PREFIX } from '../src/http/response';
import worker from '../src/index';

afterEach(() => vi.restoreAllMocks());

async function send(
  path: string,
  init: RequestInit = {},
  bindings: Omit<Env, 'MAINTENANCE_MODE'> & { MAINTENANCE_MODE: string } = env,
): Promise<Response> {
  const context = createExecutionContext();
  // Wrangler types this variable as the configured literal "false". Tests also
  // exercise the "true" value that operators can set for maintenance.
  const response = await worker.fetch(
    new Request(`https://updates.example${path}`, init),
    bindings as Env,
    context,
  );
  await waitOnExecutionContext(context);
  return response;
}

const RESOURCES = [
  ['/v1/telegram-updates', ['POST', 'OPTIONS']],
  ['/v1/license-pairings', ['POST', 'OPTIONS']],
  ['/v1/license-pairings/id', ['GET', 'HEAD', 'OPTIONS']],
  ['/v1/license-challenges', ['POST', 'OPTIONS']],
  ['/v1/license-leases', ['POST', 'OPTIONS']],
  ['/v1/release-channels/stable/releases/latest', ['GET', 'HEAD', 'OPTIONS']],
  ['/v1/downloads/ticket', ['GET', 'HEAD', 'OPTIONS']],
  ['/v1/push-installations', ['POST', 'OPTIONS']],
  ['/v1/push-installations/id', ['GET', 'HEAD', 'PUT', 'DELETE', 'OPTIONS']],
  ['/v1/push-subscriptions', ['POST', 'OPTIONS']],
  ['/v1/push-subscriptions/id', ['DELETE', 'OPTIONS']],
  ['/v1/push-deliveries', ['POST', 'OPTIONS']],
  ['/problems/invalid_token', ['GET', 'HEAD', 'OPTIONS']],
] as const;

describe('HTTP contract', () => {
  it.each(RESOURCES)(
    'derives OPTIONS and 405 Allow for %s without business I/O',
    async (path, methods) => {
      const query = vi.spyOn(env.DB, 'prepare').mockImplementation(() => {
        throw new Error('unexpected business query');
      });
      for (const maintenance of ['false', 'true']) {
        const bindings = { ...env, MAINTENANCE_MODE: maintenance };
        const options = await send(path, { method: 'OPTIONS' }, bindings);
        expect(options.status).toBe(204);
        expect(await options.text()).toBe('');
        expect(new Set(options.headers.get('allow')?.split(', '))).toEqual(
          new Set(methods),
        );
        const wrong = await send(path, { method: 'PATCH' }, bindings);
        expect(wrong.status).toBe(405);
        expect(new Set(wrong.headers.get('allow')?.split(', '))).toEqual(
          new Set(methods),
        );
        expect(wrong.headers.get('content-type')).toBe(PROBLEM_MEDIA_TYPE);
        expect(await wrong.json()).toMatchObject({
          type: PROBLEM_TYPE_PREFIX + 'method_not_allowed',
          status: 405,
        });
      }
      expect(query).not.toHaveBeenCalled();
    },
  );

  it.each(['/unknown', '/v1/license-pairings/', '/v1/push-installations/a/b'])(
    'keeps strict unknown paths at 404: %s',
    async (path) => {
      for (const method of ['GET', 'OPTIONS']) {
        const response = await send(path, { method });
        expect(response.status).toBe(404);
        expect(response.headers.get('allow')).toBeNull();
        expect(await response.json()).toMatchObject({
          error_code: 'resource_not_found',
        });
      }
    },
  );

  it('keeps business 404 and identifies methods the server does not implement', async () => {
    const business = await send('/v1/license-pairings/missing');
    expect(business.status).toBe(404);
    expect(business.headers.get('allow')).toBeNull();
    expect(await business.json()).toMatchObject({
      error_code: 'license_pairing_not_found',
    });
    const context = createExecutionContext();
    const request = new Request('https://updates.example/v1/license-pairings');
    // workerd rejects extension methods in Request's constructor. Exercise the
    // application boundary for platforms that do forward those methods.
    Object.defineProperty(request, 'method', { value: 'BREW' });
    const extension = await worker.fetch(request, env, context);
    await waitOnExecutionContext(context);
    expect(extension.status).toBe(501);
    expect(await extension.json()).toMatchObject({
      error_code: 'method_not_implemented',
    });
  });

  it.each([
    ['empty', undefined, 'application/json', 400],
    ['malformed', '{', 'application/json', 400],
    ['trailing', '{} {}', 'application/json', 400],
    ['media', '{}', 'text/plain', 415],
    ['fields', '{}', 'application/json', 422],
    ['wrong type', '[]', 'application/json', 422],
  ] as const)(
    'classifies %s request bodies',
    async (scenario, body, media, status) => {
      const response = await send('/v1/license-challenges', {
        method: 'POST',
        headers: { 'content-type': media },
        body,
      });
      expect(response.status).toBe(status);
      expect(await response.json()).toMatchObject({ status });
    },
  );

  it('bounds an undeclared body and cancels the stream before business work', async () => {
    let cancelled = false;
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        controller.enqueue(new Uint8Array(4096));
      },
      cancel() {
        cancelled = true;
      },
    });
    const response = await send('/v1/license-challenges', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body,
    });
    expect(response.status).toBe(413);
    expect(cancelled).toBe(true);
  });

  it('preserves the size-limit error when stream cancellation rejects', async () => {
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        controller.enqueue(new Uint8Array(65_537));
      },
      cancel() {
        throw new Error('cancel rejected');
      },
    });
    const response = await send('/v1/license-challenges', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body,
    });
    expect(response.status).toBe(413);
  });

  it.each(['application/json', 'application/problem+json', '*/*'])(
    'always returns Problem Details for Accept %s',
    async (accept) => {
      const response = await send('/absent', {
        headers: { accept, 'x-request-id': 'untrusted-id' },
      });
      const id = response.headers.get('x-request-id');
      expect(id).toBeTruthy();
      expect(id).not.toBe('untrusted-id');
      expect(response.headers.get('content-type')).toBe(PROBLEM_MEDIA_TYPE);
      expect(response.headers.get('cache-control')).toBe('no-store');
      const problem = await response.json();
      expect(problem).toMatchObject({
        type: PROBLEM_TYPE_PREFIX + 'resource_not_found',
        title: 'Not Found',
        status: 404,
        request_id: id,
      });
      expect(problem).not.toHaveProperty('message');
    },
  );

  it('keeps maintenance responses structured with a retry deadline', async () => {
    const response = await send(
      '/v1/license-challenges',
      { method: 'POST' },
      { ...env, MAINTENANCE_MODE: 'true' },
    );
    expect(response.status).toBe(503);
    expect(response.headers.get('retry-after')).toBe('30');
    expect(await response.json()).toMatchObject({
      error_code: 'database_maintenance',
      request_id: response.headers.get('x-request-id'),
    });
  });

  it('preserves framework status and relevant headers without returning exception details', async () => {
    vi.spyOn(Database.prototype, 'findPairing').mockRejectedValue(
      new HTTPException(429, {
        res: new Response('private exception', {
          status: 429,
          headers: { 'retry-after': '90' },
        }),
      }),
    );
    const response = await send('/v1/license-pairings/test');
    expect(response.status).toBe(429);
    expect(response.headers.get('retry-after')).toBe('90');
    expect(await response.text()).not.toContain('private exception');
  });

  it('normalizes unexpected exceptions and keeps HEAD failures bodyless', async () => {
    vi.spyOn(Database.prototype, 'findPairing').mockRejectedValue(
      new Error('private SQL and token'),
    );
    for (const method of ['GET', 'HEAD']) {
      const response = await send('/v1/license-pairings/test', { method });
      expect(response.status).toBe(500);
      const body = await response.text();
      expect(body).not.toContain('private SQL and token');
      if (method === 'HEAD') {
        expect(body).toBe('');
      }
    }
  });

  it('provides the authentication challenge for the resource being called', async () => {
    const push = await send('/v1/push-installations/id');
    expect(push.status).toBe(401);
    expect(push.headers.get('www-authenticate')).toBe(
      'Bearer realm="sigmo-push"',
    );
    const release = await send('/v1/release-channels/stable/releases/latest');
    expect(release.status).toBe(401);
    expect(release.headers.get('www-authenticate')).toBe(
      'SigmoLease realm="sigmo-pro"',
    );
  });

  it('serves the same registered type on either hostname', async () => {
    const response = await send('/problems/invalid_token');
    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({
      type: PROBLEM_TYPE_PREFIX + 'invalid_token',
      error_code: 'invalid_token',
    });
    const head = await send('/problems/invalid_token', { method: 'HEAD' });
    expect(head.status).toBe(200);
    expect(await head.text()).toBe('');
    expect((await send('/problems/invalid_token%2Funknown')).status).toBe(404);
  });
});
