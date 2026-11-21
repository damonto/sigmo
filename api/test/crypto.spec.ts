import { env } from 'cloudflare:workers';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { TEXT_ENCODER, verifyEd25519 } from '../src/security/crypto';

afterEach(() => vi.restoreAllMocks());

describe('signature verification', () => {
  it('treats rejected Ed25519 verification as an invalid signature', async () => {
    vi.spyOn(crypto.subtle, 'verify').mockRejectedValue(
      new Error('verification rejected'),
    );
    const result = verifyEd25519(
      env.SIGMO_LICENSE_PUBLIC_KEY,
      TEXT_ENCODER.encode('message'),
      'AA',
    );
    await expect(result).resolves.toBe(false);
  });
});
