import {
  createExecutionContext,
  waitOnExecutionContext,
} from 'cloudflare:test';
import { env } from 'cloudflare:workers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { randomToken, sha256 } from '../src/security/crypto';
import worker from '../src/index';
import { PushDatabase } from '../src/storage/push';
import type { PushAlertDelivery, PushDelivery } from '../src/push/types';

const IncomingRequest = Request<unknown, IncomingRequestCfProperties>;

describe('VoIP channels', () => {
  it('registers without alert permission and rotates concurrent channels independently', async () => {
    const id = crypto.randomUUID();
    const token = randomToken(32);
    const common = { id, environment: 'development', label: 'Calls only' };
    expect(
      (
        await request('push-installations', 'POST', token, {
          ...common,
          voipToken: 'a'.repeat(64),
        })
      ).status,
    ).toBe(201);
    const db = new PushDatabase(env.DB);
    const before = await db.installation(id);
    expect(before?.deviceToken).toBe('');
    const results = await Promise.all([
      request('push-installations', 'POST', token, {
        ...common,
        voipToken: 'b'.repeat(64),
      }),
      request('push-installations', 'POST', token, {
        ...common,
        deviceToken: 'c'.repeat(64),
      }),
    ]);
    expect(results.map((response) => response.status)).toEqual([200, 200]);
    const current = await db.installation(id);
    expect(current?.voipToken).toBe('b'.repeat(64));
    expect(current?.deviceToken).toBe('c'.repeat(64));
    if (!before || !current) {
      throw new Error('installation missing');
    }
    expect(await db.invalidateToken(before, true)).toBe(false);
    expect(await db.invalidateToken(current, true)).toBe(true);
    expect((await db.installation(id))?.deviceToken).toBe('c'.repeat(64));
  });

  it('rejects an empty new registration and clears the other channel on environment changes', async () => {
    expect(
      (
        await request('push-installations', 'POST', randomToken(32), {
          id: crypto.randomUUID(),
          environment: 'development',
          label: 'empty',
          voipToken: '',
        })
      ).status,
    ).toBe(422);
    const owner = await installation();
    expect(
      (
        await request('push-installations', 'POST', owner.token, {
          id: owner.id,
          environment: 'production',
          label: 'TestFlight',
          voipToken: 'd'.repeat(64),
        })
      ).status,
    ).toBe(200);
    const saved = await new PushDatabase(env.DB).installation(owner.id);
    expect(saved?.deviceToken).toBe('');
    expect(saved?.voipToken).toBe('d'.repeat(64));
    expect(saved?.tokenRevision).toBe(2);
  });

  it('uses VoIP headers, expires immediately, deduplicates calls, and leaves badges unchanged', async () => {
    const owner = await installation();
    await request('push-installations', 'POST', owner.token, {
      ...owner.body,
      voipToken: 'e'.repeat(64),
    });
    const sub = await subscription(owner);
    await request('push-subscriptions', 'POST', owner.token, {
      ...sub.body,
      calls: true,
    });
    const db = new PushDatabase(env.DB);
    const before = await db.badge(owner.id);
    const push: PushDelivery = {
      ...delivery(sub.id, 999, 100),
      kind: 'call',
      callUUID: crypto.randomUUID(),
    };
    const fetch = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 200 }));
    expect(
      (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
    ).toBe(200);
    expect(
      (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
    ).toBe(200);
    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, options] = fetch.mock.calls[0];
    const outgoing = new Request(url, options);
    expect(outgoing.url).toContain('e'.repeat(64));
    const headers = new Headers(options?.headers);
    expect(headers.get('apns-topic')).toBe('com.damon.sigmo.voip');
    expect(headers.get('apns-push-type')).toBe('voip');
    expect(headers.get('apns-priority')).toBe('10');
    expect(headers.get('apns-expiration')).toBe('0');
    expect(await outgoing.json()).toMatchObject({
      aps: {},
      callUUID: push.callUUID,
    });
    expect(await db.badge(owner.id)).toEqual(before);
    await request('push-deliveries', 'POST', sub.sendToken, {
      ...push,
      expiresAt: 1,
    });
    await request('push-subscriptions', 'POST', owner.token, {
      ...sub.body,
      calls: false,
    });
    await request('push-deliveries', 'POST', sub.sendToken, {
      ...push,
      eventId: 'second-call',
      envelope: { ...push.envelope, eventId: 'second-call' },
    });
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('invalidates only a failed VoIP token and keeps the SMS grant', async () => {
    const owner = await installation();
    await request('push-installations', 'POST', owner.token, {
      ...owner.body,
      voipToken: 'f'.repeat(64),
    });
    const sub = await subscription(owner);
    await request('push-subscriptions', 'POST', owner.token, {
      ...sub.body,
      calls: true,
    });
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json({ reason: 'Unregistered' }, { status: 410 }),
    );
    expect(
      (
        await request('push-deliveries', 'POST', sub.sendToken, {
          ...delivery(sub.id),
          kind: 'call',
          callUUID: crypto.randomUUID(),
        })
      ).status,
    ).toBe(200);
    const db = new PushDatabase(env.DB);
    expect((await db.installation(owner.id))?.voipToken).toBe('');
    expect((await db.installation(owner.id))?.deviceToken).toBe(
      owner.body.deviceToken,
    );
    expect(await db.subscription(sub.id)).not.toBeNull();
  });
});

beforeEach(async () => {
  await env.DB.prepare('DELETE FROM push_installations').run();
  await env.DB.prepare('DELETE FROM push_rate_limits').run();
});
afterEach(() => vi.restoreAllMocks());

async function request(
  path: string,
  method: string,
  token: string,
  body?: object,
): Promise<Response> {
  const ctx = createExecutionContext();
  const response = await worker.fetch(
    new IncomingRequest(`https://gateway.example/v1/${path}`, {
      method,
      headers: {
        authorization: `Bearer ${token}`,
        'content-type': 'application/json',
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
    env,
    ctx,
  );
  await waitOnExecutionContext(ctx);
  return response;
}

async function installation(
  environment: 'development' | 'production' = 'development',
) {
  const id = crypto.randomUUID();
  const token = randomToken(32);
  const body = {
    id,
    deviceToken: crypto.randomUUID().replaceAll('-', '').repeat(2),
    environment,
    label: 'Test iPhone',
  };
  const result = await request('push-installations', 'POST', token, body);
  expect(result.status).toBe(201);
  return { id, token, body };
}

async function subscription(
  owner: Awaited<ReturnType<typeof installation>>,
  backendId = crypto.randomUUID(),
) {
  const id = crypto.randomUUID();
  const sendToken = randomToken(32);
  const body = {
    id,
    installationId: owner.id,
    backendId,
    sendToken,
    enabled: true,
    sms: true,
    reminders: true,
  };
  expect(
    (await request('push-subscriptions', 'POST', owner.token, body)).status,
  ).toBe(201);
  return { id, sendToken, body };
}

function delivery(
  subscriptionId: string,
  revision = 1,
  unreadCount = 1,
): PushAlertDelivery {
  const eventId = `sms:${crypto.randomUUID()}`;
  return {
    subscriptionId,
    eventId,
    kind: 'sms',
    revision,
    unreadCount,
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    threadId: 'opaque-thread',
    envelope: {
      version: 1,
      subscriptionId,
      eventId,
      nonce: btoa('\0'.repeat(12)),
      ciphertext: btoa('opaque authenticated bytes'.repeat(4)),
    },
  };
}

describe('base push registration', () => {
  it.each(['deviceToken', 'voipToken'])(
    'keeps the previous registration when updating to an occupied %s',
    async (channel) => {
      const owner = await installation();
      const competitor = await installation();
      const token =
        channel === 'deviceToken' ? owner.body.deviceToken : 'c'.repeat(64);
      if (channel === 'voipToken') {
        expect(
          (
            await request('push-installations', 'POST', owner.token, {
              ...owner.body,
              voipToken: token,
            })
          ).status,
        ).toBe(200);
      }
      const db = new PushDatabase(env.DB);
      const before = await db.installation(competitor.id);
      const response = await request(
        `push-installations/${competitor.id}`,
        'PUT',
        competitor.token,
        {
          ...competitor.body,
          [channel]: token,
        },
      );

      expect(response.status).toBe(409);
      expect(await db.installation(competitor.id)).toEqual(before);
    },
  );

  it.each(['deviceToken', 'voipToken'])(
    'returns a conflict for concurrent claims of the same %s',
    async (channel) => {
      const deviceToken = 'b'.repeat(64);
      const responses = await Promise.all(
        Array.from({ length: 2 }, () =>
          request('push-installations', 'POST', randomToken(32), {
            id: crypto.randomUUID(),
            environment: 'development',
            label: 'Competing registration',
            [channel]: deviceToken,
          }),
        ),
      );
      expect(
        responses
          .map((response) => response.status)
          .sort((left, right) => left - right),
      ).toEqual([201, 409]);
    },
  );

  it.each([
    'TOKEN',
    'Basic TOKEN',
    'Bearer\tTOKEN',
    'Bearer TOKEN, Bearer TOKEN',
  ])('rejects malformed authorization: %s', async (authorization) => {
    const owner = await installation();
    const context = createExecutionContext();
    const response = await worker.fetch(
      new IncomingRequest(
        `https://gateway.example/v1/push-installations/${owner.id}`,
        {
          headers: {
            authorization: authorization.replaceAll('TOKEN', owner.token),
          },
        },
      ),
      env,
      context,
    );
    await waitOnExecutionContext(context);
    expect(response.status).toBe(401);
  });

  it('registers without a Pro lease and prevents takeover', async () => {
    const owner = await installation();
    expect(
      (await request('push-installations', 'POST', owner.token, owner.body))
        .status,
    ).toBe(200);
    expect(
      (await request('push-installations', 'POST', randomToken(32), owner.body))
        .status,
    ).toBe(401);
    const sub = await subscription(owner);
    const attacker = await installation();
    expect(
      (
        await request('push-subscriptions', 'POST', attacker.token, {
          ...sub.body,
          installationId: attacker.id,
        })
      ).status,
    ).toBe(401);
    expect(
      (await request(`push-subscriptions/${sub.id}`, 'DELETE', attacker.token))
        .status,
    ).toBe(401);
    expect(
      (await request(`push-subscriptions/${sub.id}`, 'DELETE', owner.token))
        .status,
    ).toBe(204);
    expect(
      (
        await request(
          'push-deliveries',
          'POST',
          sub.sendToken,
          delivery(sub.id),
        )
      ).status,
    ).toBe(401);
  });

  it('limits registration and rejects a plaintext notification', async () => {
    const owner = await installation();
    const sub = await subscription(owner);
    expect(
      (
        await request('push-deliveries', 'POST', sub.sendToken, {
          ...delivery(sub.id),
          envelope: undefined,
          text: 'secret SMS',
        })
      ).status,
    ).toBe(422);
    const db = new PushDatabase(env.DB);
    const key = `registration:${await sha256('local')}`;
    for (let i = 0; i < 20; i++) {
      await db.rateLimit(key, 20, 600);
    }
    const id = crypto.randomUUID();
    const limited = await request(
      'push-installations',
      'POST',
      randomToken(32),
      {
        ...owner.body,
        id,
        deviceToken: 'a'.repeat(64),
      },
    );
    expect(limited.status).toBe(429);
    const wait = Number(limited.headers.get('retry-after'));
    const expectedWait = Math.ceil((600_000 - (Date.now() % 600_000)) / 1000);
    expect(wait).toBeGreaterThanOrEqual(expectedWait);
    expect(wait).toBeLessThanOrEqual(expectedWait + 1);
  });
});

describe('APNs relay', () => {
  it.each([
    [403, 'ExpiredProviderToken', 503, 'push_provider_authentication_failed'],
    [429, 'TooManyRequests', 429, 'push_rate_limited'],
    [429, 'TooManyProviderTokenUpdates', 503, 'push_provider_unavailable'],
    [500, 'InternalServerError', 503, 'push_provider_unavailable'],
    [400, 'BadPayload', 502, 'push_provider_rejected'],
    [413, 'PayloadTooLarge', 422, 'push_payload_too_large'],
    [503, 'Unregistered', 503, 'push_provider_unavailable'],
  ] as const)(
    'maps APNs %s %s without revoking valid credentials',
    async (status, reason, expected, code) => {
      const owner = await installation();
      const sub = await subscription(owner);
      vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
        Response.json({ reason }, { status, headers: { 'retry-after': '90' } }),
      );
      const response = await request(
        'push-deliveries',
        'POST',
        sub.sendToken,
        delivery(sub.id),
      );
      expect(response.status).toBe(expected);
      expect(await response.json()).toMatchObject({
        error_code: code,
        status: expected,
      });
      if (expected === 429 || expected === 503) {
        expect(response.headers.get('retry-after')).toBe('90');
      }
      expect(
        (await new PushDatabase(env.DB).installation(owner.id))?.deviceToken,
      ).toBe(owner.body.deviceToken);
    },
  );

  it.each([
    ['HTML', '<html>upstream failed</html>', 'text/html'],
    ['broken JSON', '{', 'application/json'],
    ['oversized JSON', 'x'.repeat(65_537), 'application/json'],
  ])(
    'treats %s APNs data as a provider fault',
    async (scenario, body, media) => {
      const owner = await installation();
      const sub = await subscription(owner);
      vi.spyOn(globalThis, 'fetch').mockImplementation(
        async () =>
          new Response(body, {
            status: 410,
            headers: { 'content-type': media, 'retry-after': '90' },
          }),
      );
      const response = await request(
        'push-deliveries',
        'POST',
        sub.sendToken,
        delivery(sub.id),
      );
      expect(response.status).toBe(502);
      expect(response.headers.get('retry-after')).toBe('90');
      expect(await response.json()).toMatchObject({
        error_code: 'push_provider_response_invalid',
      });
      expect(
        (await new PushDatabase(env.DB).installation(owner.id))?.deviceToken,
      ).toBe(owner.body.deviceToken);
    },
  );

  it.each(['development', 'production'] as const)(
    'uses the %s endpoint and deduplicates accepted events',
    async (environment) => {
      const owner = await installation(environment);
      const sub = await subscription(owner);
      const push = delivery(sub.id);
      const fetch = vi
        .spyOn(globalThis, 'fetch')
        .mockImplementation(async (input, init) => {
          // Keep runtime Request validation even when APNs itself is replaced.
          new Request(input, init);
          return new Response(null, { status: 200 });
        });
      expect(
        (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
      ).toBe(200);
      expect(
        (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
      ).toBe(200);
      expect(fetch).toHaveBeenCalledTimes(1);
      const [address, init] = fetch.mock.calls[0];
      const outgoing = new Request(address, init);
      expect(outgoing.url).toContain(
        environment === 'development'
          ? 'api.sandbox.push.apple.com'
          : 'api.push.apple.com',
      );
      expect(new Headers(init?.headers).get('apns-topic')).toBe(
        'com.damon.sigmo',
      );
      expect(new Headers(init?.headers).get('apns-push-type')).toBe('alert');
      expect(new Headers(init?.headers).get('authorization')).toMatch(
        /^bearer [^.]+\.[^.]+\.[^.]+$/,
      );
      expect(await outgoing.json()).toMatchObject({
        sigmo: push.envelope,
        aps: { 'mutable-content': 1, badge: 1 },
      });
    },
  );

  it.each([301, 302, 303, 307, 308])(
    'rejects an APNs %s redirect without forwarding credentials',
    async (status) => {
      const owner = await installation();
      const sub = await subscription(owner);
      const fetch = vi
        .spyOn(globalThis, 'fetch')
        .mockImplementation(async (input, init) => {
          const outgoing = new Request(input, init);
          expect(outgoing.redirect).toBe('manual');
          return new Response(null, {
            status,
            headers: { location: 'https://unexpected.example/push' },
          });
        });
      const response = await request(
        'push-deliveries',
        'POST',
        sub.sendToken,
        delivery(sub.id),
      );
      expect(response.status).toBe(502);
      expect(fetch).toHaveBeenCalledTimes(1);
      expect(
        (await new PushDatabase(env.DB).installation(owner.id))?.deviceToken,
      ).toBe(owner.body.deviceToken);
    },
  );

  it('sums distinct backends and ignores an older unread snapshot', async () => {
    const owner = await installation();
    const first = await subscription(owner);
    const second = await subscription(owner);
    const fetch = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => new Response(null, { status: 200 }));
    for (const [sub, revision, count] of [
      [first, 2, 3],
      [second, 3, 4],
      [first, 1, 1],
    ] as const) {
      const response = await request(
        'push-deliveries',
        'POST',
        sub.sendToken,
        delivery(sub.id, revision, count),
      );
      expect(response.status).toBe(200);
    }
    const [address, init] = fetch.mock.calls[2];
    expect(await new Request(address, init).json()).toMatchObject({
      aps: { badge: 7 },
    });
    expect((await new PushDatabase(env.DB).badge(owner.id)).count).toBe(7);
  });

  it('retries temporary failures while retaining the event identity', async () => {
    const owner = await installation();
    const sub = await subscription(owner);
    const push = delivery(sub.id);
    const fetch = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        Response.json({ reason: 'ServiceUnavailable' }, { status: 503 }),
      )
      .mockResolvedValueOnce(new Response(null, { status: 200 }));
    expect(
      (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
    ).toBe(503);
    expect(
      (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
    ).toBe(200);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(
      new Headers(fetch.mock.calls[0][1]?.headers).get('apns-collapse-id'),
    ).toBe(
      new Headers(fetch.mock.calls[1][1]?.headers).get('apns-collapse-id'),
    );
  });

  it('does not let an old APNs error delete a rotated token', async () => {
    const owner = await installation();
    const sub = await subscription(owner);
    const newToken = 'f'.repeat(64);
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
      expect(
        (
          await request(`push-installations/${owner.id}`, 'PUT', owner.token, {
            ...owner.body,
            deviceToken: newToken,
          })
        ).status,
      ).toBe(200);
      return Response.json({ reason: 'Unregistered' }, { status: 410 });
    });
    expect(
      (
        await request(
          'push-deliveries',
          'POST',
          sub.sendToken,
          delivery(sub.id),
        )
      ).status,
    ).toBe(503);
    expect(
      (await new PushDatabase(env.DB).installation(owner.id))?.deviceToken,
    ).toBe(newToken);
  });

  it('prevents concurrent delivery attempts from both reaching APNs', async () => {
    const owner = await installation();
    const sub = await subscription(owner);
    const push = delivery(sub.id);
    const db = new PushDatabase(env.DB);
    expect(await db.claim(push, 'first-lease')).toBe('claimed');
    const fetch = vi.spyOn(globalThis, 'fetch');
    expect(
      (await request('push-deliveries', 'POST', sub.sendToken, push)).status,
    ).toBe(409);
    expect(fetch).not.toHaveBeenCalled();
  });
});
