import { Hono } from 'hono';
import type { Context, Handler } from 'hono';
import { HTTPException } from 'hono/http-exception';
import { matchedRoutes } from 'hono/route';
import { methodNotAllowed as checkMethod } from 'hono/method-not-allowed';

import type { RequestContext } from './context';
import { Database } from '../storage/authorization';
import {
  apiError,
  finishResponse,
  methodNotAllowed,
  problemDescription,
} from './response';
import { logError } from '../observability/log';
import { RequestBodyError } from './body';
import type { ProblemCode } from './response';
import {
  createChallenge,
  createLease,
  createPairing,
  getPairing,
} from '../license/service';
import {
  deleteInstallation,
  deleteSubscription,
  deliver,
  getInstallation,
  registerInstallation,
  registerSubscription,
} from '../push/service';
import { PushDatabase } from '../storage/push';
import { download, latestRelease } from '../releases/service';
import { telegramWebhook } from '../telegram/webhook';

interface ApiEnv {
  Bindings: Env;
  Variables: { requestId: string };
}
export const app = new Hono<ApiEnv>({ strict: true });

app.use(async (c, next) => {
  const requestId = crypto.randomUUID();
  c.set('requestId', requestId);
  const started = Date.now();
  // Known HTTP methods on the wrong resource are 405. Unimplemented extension
  // methods are 501 when the platform lets them reach the application.
  if (
    ![
      'GET',
      'HEAD',
      'POST',
      'PUT',
      'PATCH',
      'DELETE',
      'OPTIONS',
      'TRACE',
      'CONNECT',
    ].includes(c.req.method)
  ) {
    c.res = finishResponse(
      apiError('method_not_implemented', 'HTTP method is not implemented', 501),
      requestId,
    );
  } else {
    await next();
  }
  c.res.headers.set('x-request-id', requestId);
  if (!c.res.headers.has('cache-control')) {
    c.res.headers.set('cache-control', 'no-store');
  }
  console.info(
    JSON.stringify({
      request_id: requestId,
      method: c.req.method,
      route: matchedRoutes(c).at(-1)?.path ?? 'unmatched',
      status: c.res.status,
      duration_ms: Date.now() - started,
    }),
  );
});

app.use(
  checkMethod({
    app,
    onMethodNotAllowed: (c, methods) =>
      finishResponse(methodNotAllowed(...methods), c.get('requestId')),
  }),
);

function business(
  handler: (c: Context<ApiEnv>, context: RequestContext) => Promise<Response>,
): Handler<ApiEnv> {
  return async (c) => {
    if (String(c.env.MAINTENANCE_MODE) === 'true') {
      const response = apiError(
        'database_maintenance',
        'database maintenance in progress',
        503,
      );
      response.headers.set('retry-after', '30');
      return finishResponse(response, c.get('requestId'));
    }
    const context: RequestContext = {
      env: c.env,
      db: new Database(c.env.DB),
      execution: c.executionCtx,
    };
    const response = await handler(c, context);
    if (response.status === 401) {
      response.headers.set(
        'www-authenticate',
        c.req.path.startsWith('/v1/push-')
          ? 'Bearer realm="sigmo-push"'
          : 'SigmoLease realm="sigmo-pro"',
      );
    }
    return finishResponse(response, c.get('requestId'));
  };
}

function routeParameter(c: Context<ApiEnv>, name: string): string {
  const value = c.req.param(name);
  if (value === undefined) {
    throw new HTTPException(400);
  }
  return value;
}

app.post(
  '/v1/telegram-updates',
  business((c, ctx) => telegramWebhook(c.req.raw, ctx)),
);
app.post(
  '/v1/license-pairings',
  business((c, ctx) => createPairing(c.req.raw, ctx)),
);
app.get(
  '/v1/license-pairings/:id',
  business((c, ctx) => getPairing(c.req.raw, ctx, routeParameter(c, 'id'))),
);
app.post(
  '/v1/license-challenges',
  business((c, ctx) => createChallenge(c.req.raw, ctx)),
);
app.post(
  '/v1/license-leases',
  business((c, ctx) => createLease(c.req.raw, ctx)),
);
app.get(
  '/v1/release-channels/:channel/releases/latest',
  business((c, ctx) =>
    latestRelease(c.req.raw, ctx, routeParameter(c, 'channel')),
  ),
);
app.get(
  '/v1/downloads/:ticket',
  business((c, ctx) => download(c.req.raw, ctx, routeParameter(c, 'ticket'))),
);
app.post(
  '/v1/push-installations',
  business((c, ctx) =>
    registerInstallation(c.req.raw, new PushDatabase(ctx.env.DB)),
  ),
);
app.get(
  '/v1/push-installations/:id{[A-Za-z0-9_-]+}',
  business((c, ctx) =>
    getInstallation(
      c.req.raw,
      new PushDatabase(ctx.env.DB),
      routeParameter(c, 'id'),
    ),
  ),
);
app.put(
  '/v1/push-installations/:id{[A-Za-z0-9_-]+}',
  business((c, ctx) =>
    registerInstallation(
      c.req.raw,
      new PushDatabase(ctx.env.DB),
      routeParameter(c, 'id'),
    ),
  ),
);
app.delete(
  '/v1/push-installations/:id{[A-Za-z0-9_-]+}',
  business((c, ctx) =>
    deleteInstallation(
      c.req.raw,
      new PushDatabase(ctx.env.DB),
      routeParameter(c, 'id'),
    ),
  ),
);
app.post(
  '/v1/push-subscriptions',
  business((c, ctx) =>
    registerSubscription(c.req.raw, new PushDatabase(ctx.env.DB)),
  ),
);
app.delete(
  '/v1/push-subscriptions/:id{[A-Za-z0-9_-]+}',
  business((c, ctx) =>
    deleteSubscription(
      c.req.raw,
      new PushDatabase(ctx.env.DB),
      routeParameter(c, 'id'),
    ),
  ),
);
app.post(
  '/v1/push-deliveries',
  business((c, ctx) => deliver(c.req.raw, ctx, new PushDatabase(ctx.env.DB))),
);
app.get('/problems/:code', (c) =>
  finishResponse(problemDescription(c.req.param('code')), c.get('requestId')),
);

// Derive OPTIONS once from actual registrations. Hono performs every path match;
// there is no second router or separately maintained method list.
const methodsByPath = new Map<string, Set<string>>();
for (const route of app.routes) {
  if (route.method === 'ALL') {
    continue;
  }
  const methods = methodsByPath.get(route.path) ?? new Set<string>();
  methods.add(route.method);
  if (route.method === 'GET') {
    methods.add('HEAD');
  }
  methodsByPath.set(route.path, methods);
}
for (const [path, methods] of methodsByPath) {
  methods.add('OPTIONS');
  app.options(
    path,
    () =>
      new Response(null, {
        status: 204,
        headers: { allow: [...methods].join(', ') },
      }),
  );
}

app.notFound((c) =>
  finishResponse(
    apiError('resource_not_found', 'resource not found', 404),
    c.get('requestId'),
  ),
);
app.onError((caught, c) => {
  if (caught instanceof RequestBodyError) {
    return finishResponse(
      apiError(caught.code, caught.message, caught.status),
      c.get('requestId'),
    );
  }
  if (caught instanceof HTTPException) {
    const codes: Partial<Record<number, ProblemCode>> = {
      400: 'invalid_request',
      401: 'authorization_required',
      403: 'forbidden',
      404: 'resource_not_found',
      405: 'method_not_allowed',
      408: 'request_timeout',
      409: 'conflict',
      413: 'request_body_too_large',
      415: 'unsupported_media_type',
      422: 'unprocessable_entity',
      429: 'too_many_requests',
      500: 'internal_server_error',
      501: 'method_not_implemented',
      502: 'bad_gateway',
      503: 'service_unavailable',
      504: 'gateway_timeout',
    };
    const response = apiError(
      codes[caught.status] ?? 'remote_error',
      'HTTP request could not be completed',
      caught.status,
    );
    for (const header of ['allow', 'retry-after', 'www-authenticate']) {
      const value = caught.res?.headers.get(header);
      if (value) {
        response.headers.set(header, value);
      }
    }
    if (caught.status === 401 && !response.headers.has('www-authenticate')) {
      response.headers.set(
        'www-authenticate',
        c.req.path.startsWith('/v1/push-')
          ? 'Bearer realm="sigmo-push"'
          : 'SigmoLease realm="sigmo-pro"',
      );
    }
    if (caught.status >= 500) {
      logError('handle HTTP exception', caught, {
        request_id: c.get('requestId'),
      });
    }
    return finishResponse(response, c.get('requestId'));
  }
  logError('handle Worker request', caught, {
    request_id: c.get('requestId'),
    method: c.req.method,
    route: matchedRoutes(c).at(-1)?.path ?? 'unmatched',
  });
  return finishResponse(
    apiError('internal_server_error', 'internal server error', 500),
    c.get('requestId'),
  );
});
