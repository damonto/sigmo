import { describe, expect, it, vi } from 'vitest';
import { app } from '../src/http/app';

describe('native authentication hosting', () => {
  it('associates only the signed app and callback path', async () => {
    const response = await app.request(
      '/.well-known/apple-app-site-association',
    );
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toContain('application/json');
    expect(await response.json()).toEqual({
      applinks: {
        details: [
          {
            appIDs: ['465NH4KUMT.com.damon.sigmo'],
            components: [{ '/': '/auth/callback' }],
          },
        ],
      },
      webcredentials: { apps: ['465NH4KUMT.com.damon.sigmo'] },
    });
  });
  it('never reflects or logs callback credentials', async () => {
    const logs = vi.spyOn(console, 'info').mockImplementation(() => {});
    try {
      const response = await app.request(
        '/auth/callback?code=secret-code&state=secret-state',
      );
      expect(response.status).toBe(200);
      expect(response.headers.get('cache-control')).toBe('no-store');
      expect(response.headers.get('referrer-policy')).toBe('no-referrer');
      expect(response.headers.get('content-security-policy')).toContain(
        "default-src 'none'",
      );
      expect(await response.text()).not.toContain('secret-');
      expect(JSON.stringify(logs.mock.calls)).not.toContain('secret-');
    } finally {
      logs.mockRestore();
    }
  });
});
