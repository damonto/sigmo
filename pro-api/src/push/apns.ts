import {
  decodeBase64,
  encodeBase64,
  sha256,
  TEXT_ENCODER,
} from '../security/crypto';
import { readJson } from '../http/body';
import type { ProblemCode } from '../http/response';
import type { PushDelivery, PushInstallation } from './types';
import { isRecord } from '../http/validation';

// This cache contains provider configuration, never installation or request state.
interface ProviderToken {
  readonly fingerprint: string;
  readonly expiresAt: number;
  readonly token: string;
}

let provider: ProviderToken | undefined;

async function providerToken(env: Env): Promise<ProviderToken> {
  const fingerprint = await sha256(
    `${env.SIGMO_APNS_KEY_ID}:${env.SIGMO_APNS_TEAM_ID}:${env.SIGMO_APNS_PRIVATE_KEY}`,
  );
  if (
    provider?.fingerprint === fingerprint &&
    provider.expiresAt > Date.now()
  ) {
    return provider;
  }
  const privateKey = env.SIGMO_APNS_PRIVATE_KEY.replace(
    /-----[^-]+-----/g,
    '',
  ).replace(/\s/g, '');
  const key = await crypto.subtle.importKey(
    'pkcs8',
    decodeBase64(privateKey),
    { name: 'ECDSA', namedCurve: 'P-256' },
    false,
    ['sign'],
  );
  const header = encodeBase64(
    TEXT_ENCODER.encode(
      JSON.stringify({ alg: 'ES256', kid: env.SIGMO_APNS_KEY_ID }),
    ),
    true,
  );
  const issuedAt = Date.now();
  const claims = encodeBase64(
    TEXT_ENCODER.encode(
      JSON.stringify({
        iss: env.SIGMO_APNS_TEAM_ID,
        iat: Math.floor(issuedAt / 1000),
      }),
    ),
    true,
  );
  const signed = `${header}.${claims}`;
  const signature = await crypto.subtle.sign(
    { name: 'ECDSA', hash: 'SHA-256' },
    key,
    TEXT_ENCODER.encode(signed),
  );
  const token = `${signed}.${encodeBase64(signature, true)}`;
  if (
    provider?.fingerprint === fingerprint &&
    provider.expiresAt > Date.now()
  ) {
    return provider;
  }
  provider = { fingerprint, expiresAt: issuedAt + 40 * 60_000, token };
  return provider;
}

export interface ApnsResult {
  status: number;
  invalidToken: boolean;
  problem?: ProblemCode;
  retryAfter?: string;
}

/** sendApns sends only authenticated ciphertext and a generic fallback alert. */
export async function sendApns(
  env: Env,
  installation: PushInstallation,
  delivery: PushDelivery,
  badge: { count: number; revision: number },
): Promise<ApnsResult> {
  if (
    !env.SIGMO_APNS_PRIVATE_KEY ||
    !env.SIGMO_APNS_KEY_ID ||
    !env.SIGMO_APNS_TEAM_ID
  ) {
    return {
      status: 503,
      invalidToken: false,
      problem: 'push_provider_unavailable',
      retryAfter: '30',
    };
  }
  const voip = delivery.kind === 'call';
  const body = notificationBody(delivery, badge);
  if (TEXT_ENCODER.encode(body).length > 4096) {
    return {
      status: 422,
      invalidToken: false,
      problem: 'push_payload_too_large',
    };
  }
  const host =
    installation.environment === 'development'
      ? 'api.sandbox.push.apple.com'
      : 'api.push.apple.com';
  const collapseId =
    delivery.kind === 'badge'
      ? 'sigmo-badge'
      : await sha256(`${delivery.subscriptionId}:${delivery.eventId}`);
  let credentials: ProviderToken;
  try {
    credentials = await providerToken(env);
  } catch {
    return {
      status: 503,
      invalidToken: false,
      problem: 'push_provider_authentication_failed',
      retryAfter: '60',
    };
  }
  let response: Response;
  const signal = AbortSignal.timeout(10_000);
  try {
    response = await fetch(
      `https://${host}/3/device/${voip ? installation.voipToken : installation.deviceToken}`,
      {
        method: 'POST',
        // Workers requires manual mode to reject redirects without forwarding the JWT.
        redirect: 'manual',
        signal,
        headers: {
          authorization: `bearer ${credentials.token}`,
          'content-type': 'application/json',
          'apns-topic': voip ? `${env.PUSH_TOPIC}.voip` : env.PUSH_TOPIC,
          'apns-push-type': voip ? 'voip' : 'alert',
          'apns-priority': delivery.kind === 'badge' ? '5' : '10',
          'apns-expiration': voip ? '0' : String(delivery.expiresAt),
          'apns-collapse-id': collapseId,
        },
        body,
      },
    );
  } catch {
    if (signal.aborted) {
      return { status: 504, invalidToken: false, problem: 'upstream_timeout' };
    }
    return {
      status: 503,
      invalidToken: false,
      problem: 'push_provider_unavailable',
      retryAfter: '30',
    };
  }
  if (response.ok) {
    await response.body?.cancel();
    return { status: response.status, invalidToken: false };
  }
  const retryAfter = response.headers.get('retry-after') ?? undefined;
  let error: unknown;
  try {
    if (
      response.headers
        .get('content-type')
        ?.split(';')[0]
        .trim()
        .toLowerCase() !== 'application/json'
    ) {
      await response.body?.cancel();
      throw new Error('unexpected APNs error media type');
    }
    error = await readJson(response.body);
  } catch {
    if (signal.aborted) {
      return { status: 504, invalidToken: false, problem: 'upstream_timeout' };
    }
    // A provider's malformed/oversized response is never a client body error.
    return {
      status: 502,
      invalidToken: false,
      problem: 'push_provider_response_invalid',
      retryAfter,
    };
  }
  const reason =
    isRecord(error) && typeof error.reason === 'string' ? error.reason : '';
  if (!reason) {
    return {
      status: 502,
      invalidToken: false,
      problem: 'push_provider_response_invalid',
      retryAfter,
    };
  }
  if (
    response.status === 403 &&
    reason === 'ExpiredProviderToken' &&
    provider === credentials
  ) {
    provider = undefined;
  }
  if (
    (response.status === 410 && reason === 'Unregistered') ||
    (response.status === 400 &&
      ['BadDeviceToken', 'DeviceTokenNotForTopic'].includes(reason))
  ) {
    return { status: 410, invalidToken: true };
  }
  if (response.status === 403) {
    return {
      status: 503,
      invalidToken: false,
      problem: 'push_provider_authentication_failed',
      retryAfter: retryAfter ?? '60',
    };
  }
  if (response.status === 429 && reason === 'TooManyRequests') {
    return {
      status: 429,
      invalidToken: false,
      problem: 'push_rate_limited',
      retryAfter: retryAfter ?? '60',
    };
  }
  if (response.status >= 500 || response.status === 429) {
    return {
      status: 503,
      invalidToken: false,
      problem: 'push_provider_unavailable',
      retryAfter: retryAfter ?? '30',
    };
  }
  if (response.status === 413) {
    return {
      status: 422,
      invalidToken: false,
      problem: 'push_payload_too_large',
    };
  }
  return {
    status: 502,
    invalidToken: false,
    problem: 'push_provider_rejected',
  };
}

function notificationBody(
  delivery: PushDelivery,
  badge: { count: number; revision: number },
): string {
  if (delivery.kind === 'call') {
    return JSON.stringify({
      aps: {},
      sigmo: delivery.envelope,
      badgeRevision: badge.revision,
      callUUID: delivery.callUUID,
    });
  }
  if (delivery.kind === 'badge') {
    return JSON.stringify({
      aps: { badge: badge.count },
      badgeRevision: badge.revision,
    });
  }
  return JSON.stringify({
    aps: {
      badge: badge.count,
      sound: 'default',
      'mutable-content': 1,
      'thread-id': delivery.threadId,
      alert: {
        'title-loc-key': 'push.generic.title',
        'loc-key':
          delivery.kind === 'sms'
            ? 'push.generic.message'
            : 'push.generic.reminder',
      },
    },
    sigmo: delivery.envelope,
    badgeRevision: badge.revision,
  });
}
