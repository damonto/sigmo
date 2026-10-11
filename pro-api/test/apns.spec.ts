import { env } from 'cloudflare:workers';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { sendApns } from '../src/push/apns';
import type { PushDelivery, PushInstallation } from '../src/push/types';

afterEach(() => vi.restoreAllMocks());

const INSTALLATION: PushInstallation = {
  id: 'test-installation',
  managementHash: 'test-management-hash',
  deviceToken: 'a'.repeat(64),
  environment: 'development',
  label: 'Test iPhone',
  tokenRevision: 1,
  voipToken: '',
  voipTokenRevision: 1,
};
const DELIVERY: PushDelivery = {
  kind: 'badge',
  subscriptionId: 'test-subscription',
  eventId: 'test-event',
  revision: 1,
  unreadCount: 0,
  expiresAt: 2_000_000_000,
};
const BADGE = { count: 0, revision: 1 };

describe('APNs provider state', () => {
  it('reuses one completed provider token for concurrent sends', async () => {
    const bindings = { ...env, SIGMO_APNS_KEY_ID: crypto.randomUUID() };
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null));
    const results = await Promise.all([
      sendApns(bindings, INSTALLATION, DELIVERY, BADGE),
      sendApns(bindings, INSTALLATION, DELIVERY, BADGE),
    ]);

    expect(results.map((result) => result.status)).toEqual([200, 200]);
    const tokens = fetchMock.mock.calls.map(([input, init]) =>
      new Request(input, init).headers.get('authorization'),
    );
    expect(new Set(tokens).size).toBe(1);
  });

  it('keeps a newer provider token when an older request reports expiry', async () => {
    const oldEnv = { ...env, SIGMO_APNS_KEY_ID: crypto.randomUUID() };
    const newEnv = { ...env, SIGMO_APNS_KEY_ID: crypto.randomUUID() };
    const sign = vi.spyOn(crypto.subtle, 'sign');
    const fetchMock = vi.spyOn(globalThis, 'fetch');
    fetchMock.mockResolvedValue(new Response(null));
    fetchMock.mockImplementationOnce(async () => {
      expect(
        (await sendApns(newEnv, INSTALLATION, DELIVERY, BADGE)).status,
      ).toBe(200);
      return Response.json({ reason: 'ExpiredProviderToken' }, { status: 403 });
    });

    expect((await sendApns(oldEnv, INSTALLATION, DELIVERY, BADGE)).status).toBe(
      503,
    );
    expect((await sendApns(newEnv, INSTALLATION, DELIVERY, BADGE)).status).toBe(
      200,
    );
    expect(sign).toHaveBeenCalledTimes(2);
  });

  it('keeps a usable cached token when another configuration cannot sign', async () => {
    const validEnv = { ...env, SIGMO_APNS_KEY_ID: crypto.randomUUID() };
    const invalidEnv = { ...validEnv, SIGMO_APNS_PRIVATE_KEY: 'invalid-key' };
    const sign = vi.spyOn(crypto.subtle, 'sign');
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null));

    expect(
      (await sendApns(validEnv, INSTALLATION, DELIVERY, BADGE)).status,
    ).toBe(200);
    expect(
      (await sendApns(invalidEnv, INSTALLATION, DELIVERY, BADGE)).status,
    ).toBe(503);
    expect(
      (await sendApns(validEnv, INSTALLATION, DELIVERY, BADGE)).status,
    ).toBe(200);
    expect(sign).toHaveBeenCalledTimes(1);
  });

  it('reports a deadline while reading an upstream body as a timeout', async () => {
    const controller = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(controller.signal);
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        new ReadableStream<Uint8Array>({
          pull(stream) {
            controller.abort(new DOMException('deadline', 'TimeoutError'));
            stream.error(controller.signal.reason);
          },
        }),
        { status: 503, headers: { 'content-type': 'application/json' } },
      ),
    );

    expect(await sendApns(env, INSTALLATION, DELIVERY, BADGE)).toMatchObject({
      status: 504,
      invalidToken: false,
      problem: 'upstream_timeout',
    });
  });
});
