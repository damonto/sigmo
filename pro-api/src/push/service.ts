import type { RequestContext } from '../http/context';
import { randomToken, secureEqual, sha256 } from '../security/crypto';
import { apiError, json } from '../http/response';
import { parseJson } from '../http/body';
import { bearerToken } from '../http/authorization';
import { isRecord } from '../http/validation';
import { sendApns } from './apns';
import type { PushDatabase } from '../storage/push';
import { pushDelivery, isPushId, isPushToken } from './validation';
import type { PushInstallation, PushSubscription } from './types';

function unauthorized(): Response {
  return apiError('invalid_push_credentials', 'invalid push credentials', 401);
}
function invalid(): Response {
  return apiError('invalid_push_request', 'invalid push request', 422);
}
function limited(retryAfter: number): Response {
  const response = apiError(
    'push_rate_limited',
    'push request limit exceeded',
    429,
  );
  response.headers.set('retry-after', String(retryAfter));
  return response;
}

async function authorizeInstallation(
  request: Request,
  db: PushDatabase,
  id: string,
): Promise<PushInstallation | null> {
  const credential = bearerToken(request);
  const installation = await db.installation(id);
  if (
    !installation ||
    !isPushToken(credential) ||
    !(await secureEqual(await sha256(credential), installation.managementHash))
  ) {
    return null;
  }
  return installation;
}

export async function registerInstallation(
  request: Request,
  db: PushDatabase,
  pathId?: string,
): Promise<Response> {
  const body = await parseJson(request);
  const credential = bearerToken(request);
  if (
    !isRecord(body) ||
    !isPushId(body.id) ||
    (pathId !== undefined && pathId !== body.id) ||
    !isPushToken(credential) ||
    (body.deviceToken !== undefined &&
      (typeof body.deviceToken !== 'string' ||
        !/^[a-f0-9]{32,512}$/.test(body.deviceToken))) ||
    (body.voipToken !== undefined &&
      body.voipToken !== '' &&
      (typeof body.voipToken !== 'string' ||
        !/^[a-f0-9]{32,512}$/.test(body.voipToken))) ||
    (body.deviceToken === undefined && body.voipToken === undefined) ||
    (body.environment !== 'development' && body.environment !== 'production') ||
    typeof body.label !== 'string' ||
    body.label.length > 128
  ) {
    return invalid();
  }
  const existing = await db.installation(body.id);
  const hash = await sha256(credential);
  if (existing && !(await secureEqual(hash, existing.managementHash))) {
    return unauthorized();
  }
  if (!existing && pathId !== undefined) {
    return unauthorized();
  }
  if (!existing && !body.deviceToken && !body.voipToken) {
    return invalid();
  }
  if (!existing) {
    const retryAfter = await db.rateLimit(
      `registration:${await sha256(request.headers.get('cf-connecting-ip') ?? 'local')}`,
      20,
      600,
    );
    if (retryAfter > 0) {
      return limited(retryAfter);
    }
  }
  // Omitted channels must be preserved by SQL, not by a stale read before a
  // concurrent alert/VoIP registration. Environment changes clear old tokens.
  const deviceToken =
    typeof body.deviceToken === 'string' ? body.deviceToken : '';
  const voipToken = typeof body.voipToken === 'string' ? body.voipToken : '';
  const saved = await db.saveInstallation(
    {
      id: body.id,
      managementHash: hash,
      deviceToken,
      voipToken,
      environment: body.environment,
      label: body.label,
    },
    {
      alert: body.deviceToken !== undefined,
      voip: body.voipToken !== undefined,
    },
  );
  if (!saved) {
    const current = await db.installation(body.id);
    if (current && !(await secureEqual(hash, current.managementHash))) {
      return unauthorized();
    }
    return apiError(
      'push_token_registered',
      'device token already registered',
      409,
    );
  }
  return json(
    { id: body.id, ...(await db.badge(body.id)) },
    existing ? 200 : 201,
  );
}

export async function registerSubscription(
  request: Request,
  db: PushDatabase,
): Promise<Response> {
  const body = await parseJson(request);
  if (
    !isRecord(body) ||
    !isPushId(body.id) ||
    !isPushId(body.installationId) ||
    !isPushId(body.backendId) ||
    !isPushToken(body.sendToken) ||
    typeof body.enabled !== 'boolean' ||
    typeof body.sms !== 'boolean' ||
    typeof body.reminders !== 'boolean' ||
    (body.calls !== undefined && typeof body.calls !== 'boolean')
  ) {
    return invalid();
  }
  if (!(await authorizeInstallation(request, db, body.installationId))) {
    return unauthorized();
  }
  const retryAfter = await db.rateLimit(
    `subscription:${body.installationId}`,
    60,
    60,
  );
  if (retryAfter > 0) {
    return limited(retryAfter);
  }
  const existing = await db.subscription(body.id);
  if (
    existing &&
    (existing.installationId !== body.installationId ||
      existing.backendId !== body.backendId)
  ) {
    return unauthorized();
  }
  const subscription: PushSubscription = {
    id: body.id,
    installationId: body.installationId,
    backendId: body.backendId,
    sendHash: await sha256(body.sendToken),
    enabled: Number(body.enabled),
    sms: Number(body.sms),
    reminders: Number(body.reminders),
    calls: Number(body.calls ?? false),
  };
  await db.saveSubscription(subscription);
  const saved = await db.subscription(subscription.id);
  if (
    !saved ||
    saved.installationId !== subscription.installationId ||
    saved.backendId !== subscription.backendId ||
    !(await secureEqual(saved.sendHash, subscription.sendHash))
  ) {
    return unauthorized();
  }
  return json(
    { id: subscription.id, ...(await db.badge(subscription.installationId)) },
    existing ? 200 : 201,
  );
}

export async function deliver(
  request: Request,
  context: RequestContext,
  db: PushDatabase,
): Promise<Response> {
  const delivery = pushDelivery(await parseJson(request));
  if (!delivery) {
    return invalid();
  }
  const credential = bearerToken(request);
  const subscription = await db.subscription(delivery.subscriptionId);
  if (
    !subscription ||
    !isPushToken(credential) ||
    !(await secureEqual(await sha256(credential), subscription.sendHash))
  ) {
    return unauthorized();
  }
  const installation = await db.installation(subscription.installationId);
  if (!installation) {
    return apiError(
      'push_token_unregistered',
      'push device is no longer registered',
      410,
    );
  }
  const voip = delivery.kind === 'call';
  if (!(voip ? installation.voipToken : installation.deviceToken)) {
    if (installation.deviceToken || installation.voipToken) {
      return json({ accepted: true, suppressed: true });
    }
    return apiError(
      'push_token_unregistered',
      'push device is no longer registered',
      410,
    );
  }
  const retryAfter = await db.rateLimit(
    `delivery:${voip ? 'call:' : ''}${subscription.id}`,
    120,
    60,
  );
  if (retryAfter > 0) {
    return limited(retryAfter);
  }
  const now = Math.floor(Date.now() / 1000);
  if (delivery.expiresAt <= now) {
    return json({ accepted: true, expired: true });
  }
  delivery.expiresAt = Math.min(delivery.expiresAt, now + (voip ? 45 : 86_400));
  if (
    delivery.kind !== 'badge' &&
    (!subscription.enabled ||
      (delivery.kind === 'call' && !subscription.calls) ||
      (delivery.kind === 'sms' && !subscription.sms) ||
      (delivery.kind === 'reminder' && !subscription.reminders))
  ) {
    return json({ accepted: true, suppressed: true });
  }
  const lease = randomToken();
  const claim = await db.claim(delivery, lease);
  if (claim === 'sent') {
    return json({ accepted: true, duplicate: true });
  }
  if (claim === 'busy') {
    const response = apiError(
      'push_delivery_in_progress',
      'push delivery is in progress',
      409,
    );
    response.headers.set('retry-after', '5');
    return response;
  }
  let sent = false;
  try {
    const badge = voip
      ? await db.badge(installation.id)
      : await db.updateUnread(subscription, delivery);
    const result = await sendApns(context.env, installation, delivery, badge);
    if (result.invalidToken) {
      if (!(await db.invalidateToken(installation, voip))) {
        // The response belongs to the previous token. Keep this event retryable
        // so the backend does not revoke a grant for the newly registered token.
        return apiError(
          'push_token_rotated',
          'retry with updated device token',
          503,
        );
      }
      const current = await db.installation(installation.id);
      if (current?.deviceToken || current?.voipToken) {
        sent = true;
        return json({ accepted: true, suppressed: true });
      }
      return apiError(
        'push_token_unregistered',
        'push device is no longer registered',
        410,
      );
    }
    if (result.status < 200 || result.status >= 300) {
      const response = apiError(
        result.problem ?? 'push_provider_rejected',
        'push provider could not accept notification',
        result.status,
      );
      if (result.retryAfter) {
        response.headers.set('retry-after', result.retryAfter);
      }
      return response;
    }
    sent = true;
    return json({ accepted: true });
  } finally {
    await db.finish(delivery, lease, sent);
  }
}

export async function getInstallation(
  request: Request,
  db: PushDatabase,
  id: string,
): Promise<Response> {
  const installation = await authorizeInstallation(request, db, id);
  if (!installation) {
    return unauthorized();
  }
  if (request.method === 'HEAD') {
    return new Response(null, {
      headers: { 'content-type': 'application/json; charset=utf-8' },
    });
  }
  return json({ id: installation.id, ...(await db.badge(installation.id)) });
}

export async function deleteInstallation(
  request: Request,
  db: PushDatabase,
  id: string,
): Promise<Response> {
  const installation = await authorizeInstallation(request, db, id);
  if (!installation) {
    return unauthorized();
  }
  await db.deleteInstallation(installation.id);
  return new Response(null, { status: 204 });
}

export async function deleteSubscription(
  request: Request,
  db: PushDatabase,
  id: string,
): Promise<Response> {
  const subscription = await db.subscription(id);
  if (!subscription) {
    return new Response(null, { status: 204 });
  }
  if (
    !(await authorizeInstallation(request, db, subscription.installationId))
  ) {
    return unauthorized();
  }
  await db.deleteSubscription(subscription.id);
  return new Response(null, { status: 204 });
}
