import { CompactEncrypt, compactDecrypt } from 'jose';

import type { RequestContext } from '../http/context';
import { apiError, json } from '../http/response';
import { authorizeRequest } from '../license/service';
import {
  decodeBase64,
  encodeBase64,
  signEd25519,
  TEXT_ENCODER,
} from '../security/crypto';

const MAX_ENCRYPTED_SIZE = 6 * 1024 * 1024;
const SIGNATURE_CONTEXT = 'sigmo-carrier-config-v1\n';
const REVISION_PATTERN = /^[0-9a-f]{64}$/;

interface Pointer {
  schemaVersion: 1;
  revision: string;
}

function pointer(value: unknown): Pointer | null {
  if (
    value === null ||
    typeof value !== 'object' ||
    !('schemaVersion' in value) ||
    value.schemaVersion !== 1 ||
    !('revision' in value) ||
    typeof value.revision !== 'string' ||
    !REVISION_PATTERN.test(value.revision)
  ) {
    return null;
  }
  return { schemaVersion: 1, revision: value.revision };
}

/** configuration issues one device-bound, signed and encrypted catalog snapshot. */
export async function configuration(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const authorization = await authorizeRequest(request, context);
  if (!authorization.ok) {
    return authorization.response;
  }
  const { lease } = authorization;
  const recipient = new URL(request.url).searchParams.get('recipient') ?? '';
  let publicKey: CryptoKey;
  try {
    if (!/^[A-Za-z0-9_-]{80,160}$/.test(recipient)) {
      throw new Error('invalid recipient');
    }
    publicKey = await crypto.subtle.importKey(
      'spki',
      decodeBase64(recipient),
      { name: 'ECDH', namedCurve: 'P-256' },
      false,
      [],
    );
  } catch {
    return apiError(
      'configuration_invalid_recipient',
      'invalid configuration recipient',
      400,
    );
  }

  // D1 performs this update atomically across Worker instances. A fixed minute
  // permits retries and restarts without allowing unbounded catalog downloads.
  const minute = Math.floor(Date.now() / 60_000);
  const count = await context.db.recordConfigurationDownload(
    lease.deviceId,
    minute,
  );
  if (count === null || count > 6) {
    const response = apiError(
      'configuration_rate_limited',
      'configuration download limit exceeded',
      429,
    );
    response.headers.set(
      'retry-after',
      String(60 - (Math.floor(Date.now() / 1000) % 60)),
    );
    return response;
  }

  const current = await context.env.BUCKET.get('configs/current.json');
  if (!current || current.size > 1024) {
    if (current) {
      await current.body.cancel();
    }
    return apiError(
      'configuration_unavailable',
      'carrier configuration is unavailable',
      503,
    );
  }
  const metadata = pointer(await current.json<unknown>());
  if (!metadata) {
    throw new Error('invalid configuration pointer');
  }
  const object = await context.env.BUCKET.get(
    `configs/releases/${metadata.revision}.enc`,
  );
  if (!object || object.size > MAX_ENCRYPTED_SIZE) {
    if (object) {
      await object.body.cancel();
    }
    return apiError(
      'configuration_unavailable',
      'carrier configuration is unavailable',
      503,
    );
  }
  const key = decodeBase64(context.env.SIGMO_CONFIG_ENCRYPTION_KEY);
  if (key.byteLength !== 32) {
    throw new Error('invalid configuration storage key');
  }
  const { plaintext } = await compactDecrypt(await object.text(), key, {
    keyManagementAlgorithms: ['dir'],
    contentEncryptionAlgorithms: ['A256GCM'],
  });
  if (plaintext.byteLength > 4 * 1024 * 1024) {
    throw new Error('configuration package exceeds size limit');
  }
  const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', plaintext));
  const revision = Array.from(hash, (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');
  if (revision !== metadata.revision) {
    throw new Error('configuration revision mismatch');
  }
  const issuedAt = new Date().toISOString();
  const payload = TEXT_ENCODER.encode(
    JSON.stringify({
      schemaVersion: 1,
      deviceId: lease.deviceId,
      sessionId: lease.sessionId,
      revision,
      issuedAt,
      refreshAfter: lease.refreshAfter,
      expiresAt: lease.expiresAt,
      bundle: encodeBase64(plaintext, true),
    }),
  );
  const signatureInput = TEXT_ENCODER.encode(
    SIGNATURE_CONTEXT + new TextDecoder().decode(payload),
  );
  const signature = await signEd25519(
    context.env.SIGMO_LICENSE_PRIVATE_KEY,
    signatureInput,
  );
  const signed = TEXT_ENCODER.encode(
    JSON.stringify({
      payload: encodeBase64(payload, true),
      signature,
    }),
  );
  const encrypted = await new CompactEncrypt(signed)
    .setProtectedHeader({
      alg: 'ECDH-ES',
      enc: 'A256GCM',
      typ: 'sigmo-carrier-config+jwe',
    })
    .encrypt(publicKey);
  console.info(
    JSON.stringify({
      event: 'configuration_download',
      device_id: lease.deviceId,
      revision,
    }),
  );
  return json({ configuration: encrypted });
}
