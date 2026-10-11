import { env } from 'cloudflare:workers';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  TEXT_ENCODER,
  verifyEd25519,
  verifyHmac,
} from '../src/security/crypto';

afterEach(() => vi.restoreAllMocks());

describe('signature verification', () => {
  it.each(['Ed25519', 'HMAC'])(
    'treats rejected %s verification as an invalid signature',
    async (algorithm) => {
      vi.spyOn(crypto.subtle, 'verify').mockRejectedValue(
        new Error('verification rejected'),
      );
      const result =
        algorithm === 'Ed25519'
          ? verifyEd25519(
              env.SIGMO_LICENSE_PUBLIC_KEY,
              TEXT_ENCODER.encode('message'),
              'AA',
            )
          : verifyHmac('test-secret', 'message', 'AA');
      await expect(result).resolves.toBe(false);
    },
  );
});
