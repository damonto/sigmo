// Keep this identifier aligned with the signed iOS application's team and bundle ID.
const appID = '465NH4KUMT.com.damon.sigmo';

export function appleAssociation(): Response {
  return Response.json(
    {
      applinks: {
        details: [{ appIDs: [appID], components: [{ '/': '/auth/callback' }] }],
      },
      webcredentials: { apps: [appID] },
    },
    { headers: { 'cache-control': 'public, max-age=3600' } },
  );
}

export function nativeCallback(): Response {
  // ASWebAuthenticationSession intercepts this URL. A plain-browser fallback must
  // neither echo the authorization code nor send it to scripts, assets, or analytics.
  return new Response(
    '<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sigmo sign-in</title><body><main><h1>Return to Sigmo</h1><p>Complete sign-in in the Sigmo app. If this page remains open, close it and start sign-in again from the app.</p><p lang="zh-Hans">请返回 Sigmo App 完成登录。如果此页面没有自动关闭，请关闭页面并从 App 重新登录。</p></main></body></html>',
    {
      headers: {
        'content-type': 'text/html; charset=utf-8',
        'cache-control': 'no-store',
        'referrer-policy': 'no-referrer',
        'content-security-policy':
          "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
        'x-content-type-options': 'nosniff',
        'x-frame-options': 'DENY',
        'x-robots-tag': 'noindex, nofollow',
      },
    },
  );
}
