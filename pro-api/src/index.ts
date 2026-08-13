import type { RequestContext } from "./context";
import { Database } from "./database";
import {
  apiError,
  logError,
  methodNotAllowed,
  requestBodyTooLarge,
} from "./http";
import {
  createChallenge,
  createLease,
  createPairing,
  deviceID,
  getPairing,
  isActive,
} from "./license";
import { download, latestRelease } from "./releases";
import { displayName, telegramWebhook } from "./telegram";
import { reconcileTelegramCommands } from "./telegram_commands";

export async function route(
  request: Request,
  context: RequestContext,
): Promise<Response> {
  const url = new URL(request.url);
  if (url.pathname === "/v1/telegram-updates") {
    if (request.method !== "POST") return methodNotAllowed("POST");
    return telegramWebhook(request, context);
  }
  if (url.pathname === "/v1/license-pairings") {
    if (request.method !== "POST") return methodNotAllowed("POST");
    return createPairing(request, context);
  }

  const pairingMatch = url.pathname.match(/^\/v1\/license-pairings\/([^/]+)$/);
  if (pairingMatch) {
    if (request.method !== "GET") return methodNotAllowed("GET");
    return getPairing(request, context, pairingMatch[1]);
  }

  if (url.pathname === "/v1/license-challenges") {
    if (request.method !== "POST") return methodNotAllowed("POST");
    return createChallenge(request, context);
  }
  if (url.pathname === "/v1/license-leases") {
    if (request.method !== "POST") return methodNotAllowed("POST");
    return createLease(request, context);
  }

  const releaseMatch = url.pathname.match(
    /^\/v1\/release-channels\/([^/]+)\/releases\/latest$/,
  );
  if (releaseMatch) {
    if (request.method !== "GET") return methodNotAllowed("GET");
    return latestRelease(request, context, releaseMatch[1]);
  }

  const downloadMatch = url.pathname.match(/^\/v1\/downloads\/([^/]+)$/);
  if (downloadMatch) {
    if (request.method !== "GET") return methodNotAllowed("GET");
    return download(request, context, downloadMatch[1]);
  }

  return apiError("resource_not_found", "resource not found", 404);
}

export default {
  async fetch(
    request: Request,
    env: Env,
    ctx: ExecutionContext,
  ): Promise<Response> {
    const db = new Database(env.DB);
    const context: RequestContext = { env, db, execution: ctx };
    try {
      return await route(request, context);
    } catch (caught) {
      const url = new URL(request.url);
      if (caught === requestBodyTooLarge)
        return apiError(
          "request_body_too_large",
          "request body exceeds size limit",
          413,
        );
      logError("handle Worker request", caught, {
        method: request.method,
        path: url.pathname.startsWith("/v1/downloads/")
          ? "/v1/downloads/:ticket"
          : url.pathname,
      });
      return apiError("internal_server_error", "internal server error", 500);
    }
  },
  async scheduled(
    _controller: ScheduledController,
    env: Env,
    _ctx: ExecutionContext,
  ): Promise<void> {
    const db = new Database(env.DB);
    try {
      await reconcileTelegramCommands(env, db);
    } catch (caught) {
      logError("reconcile Telegram commands", caught);
      throw caught;
    }
  },
} satisfies ExportedHandler<Env>;

export const testExports = {
  deviceID,
  displayName,
  isActive,
};
