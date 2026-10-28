import { apiError } from '../http/response';
import { logError } from '../observability/log';
import { sha256 } from '../security/crypto';
import { readLimitedBody } from './body';
import { isPLMN, loadCarrierLogoCatalog, SIMDEX_ORIGIN } from './catalog';
import { rasterizeSvg } from './raster';

const CACHE_SECONDS = 86_400;
const CACHE_VERSION = 'v1';
const FETCH_TIMEOUT_MS = 10_000;
const MAX_ICON_BYTES = 2 * 1024 * 1024;
const TEXT_ELEMENT_PATTERN = /<(?:text|tspan|textPath)(?:\s|>)/i;

export function carrierLogoCacheKey(icon: string): Request {
  const url = new URL(icon, SIMDEX_ORIGIN);
  url.searchParams.set('sigmo-png', CACHE_VERSION);
  return new Request(url);
}

function missingLogo(): Response {
  const response = apiError(
    'resource_not_found',
    'carrier logo not found',
    404,
  );
  response.headers.set('cache-control', 'public, max-age=300');
  return response;
}

function unavailableLogo(): Response {
  const response = apiError(
    'service_unavailable',
    'carrier logo unavailable',
    503,
  );
  response.headers.set('cache-control', 'no-store');
  return response;
}

function isPng(bytes: Uint8Array): boolean {
  return (
    bytes.length >= 8 &&
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47 &&
    bytes[4] === 0x0d &&
    bytes[5] === 0x0a &&
    bytes[6] === 0x1a &&
    bytes[7] === 0x0a
  );
}

function isNotModified(request: Request, etag: string): boolean {
  const ifNoneMatch = request.headers.get('if-none-match');
  return (ifNoneMatch?.split(',') ?? []).some((tag) => {
    const normalized = tag.trim().replace(/^W\//, '');
    return normalized === '*' || normalized === etag;
  });
}

function conditionalResponse(request: Request, response: Response): Response {
  const headers = new Headers(response.headers);
  const etag = headers.get('etag');
  if (etag && isNotModified(request, etag)) {
    return new Response(null, { status: 304, headers });
  }
  if (request.method === 'HEAD') {
    return new Response(null, { status: response.status, headers });
  }
  return new Response(response.body, { status: response.status, headers });
}

async function fetchPng(icon: string): Promise<Uint8Array | null> {
  const iconURL = new URL(icon, SIMDEX_ORIGIN);
  const response = await fetch(iconURL, {
    headers: { accept: 'image/png,image/svg+xml' },
    redirect: 'manual',
    signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
  });
  if (response.status === 404 || response.status === 410) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`fetch carrier logo: HTTP ${response.status}`);
  }
  const contentType = (response.headers.get('content-type') ?? '')
    .replace(/;.*$/, '')
    .trim()
    .toLowerCase();
  const bytes = await readLimitedBody(
    response,
    MAX_ICON_BYTES,
    'simdex carrier icon',
  );

  if (iconURL.pathname.toLowerCase().endsWith('.svg')) {
    if (contentType !== 'image/svg+xml') {
      throw new Error('unexpected carrier logo content type');
    }
    const svg = new TextDecoder().decode(bytes);
    // Text logos are intentionally omitted so clients fall back to flags.
    if (TEXT_ELEMENT_PATTERN.test(svg)) {
      return null;
    }
    return rasterizeSvg(bytes);
  }

  if (contentType !== 'image/png' || !isPng(bytes)) {
    throw new Error('invalid carrier logo image');
  }
  return bytes;
}

async function pngHeaders(png: Uint8Array): Promise<Headers> {
  const etag = `"${await sha256(png)}"`;
  return new Headers({
    'access-control-allow-origin': '*',
    'cache-control': `public, max-age=${CACHE_SECONDS}`,
    'content-length': String(png.byteLength),
    'content-type': 'image/png',
    etag,
    'x-content-type-options': 'nosniff',
  });
}

/** carrierLogo proxies the current Simdex image as a cached PNG. */
export async function carrierLogo(
  request: Request,
  bucket: R2Bucket,
  plmn: string,
  execution: Pick<ExecutionContext, 'waitUntil'>,
): Promise<Response> {
  if (!isPLMN(plmn)) {
    return missingLogo();
  }

  try {
    const logos = await loadCarrierLogoCatalog(bucket);
    const icon = logos.get(plmn);
    if (!icon) {
      return missingLogo();
    }

    const cache = caches.default;
    const cacheKey = carrierLogoCacheKey(icon);
    try {
      const cached = await cache.match(cacheKey);
      if (cached) {
        if (cached.status === 404) {
          return missingLogo();
        }
        return conditionalResponse(request, cached);
      }
    } catch (caught) {
      logError('read carrier logo cache', caught, { icon, plmn });
    }

    const png = await fetchPng(icon);
    if (!png) {
      execution.waitUntil(
        cache
          .put(
            cacheKey,
            new Response(null, {
              status: 404,
              headers: { 'cache-control': 'public, max-age=300' },
            }),
          )
          .catch((caught: unknown) => {
            logError('cache missing carrier logo', caught, { icon, plmn });
          }),
      );
      return missingLogo();
    }
    const response = new Response(png, { headers: await pngHeaders(png) });
    execution.waitUntil(
      cache.put(cacheKey, response.clone()).catch((caught: unknown) => {
        logError('cache carrier logo', caught, { icon, plmn });
      }),
    );
    return conditionalResponse(request, response);
  } catch (caught) {
    logError('proxy carrier logo', caught, { plmn });
    return unavailableLogo();
  }
}
