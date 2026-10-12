import catalog from './carriers.json';
import { apiError } from '../http/response';
import { logError } from '../observability/log';

const keys = new Map(
  catalog.flatMap((carrier) =>
    carrier.plmns.map((plmn) => [plmn, `logos/${carrier.logo}`] as const),
  ),
);

function missingLogo(): Response {
  const response = apiError(
    'resource_not_found',
    'carrier logo not found',
    404,
  );
  response.headers.set('cache-control', 'public, max-age=300');
  return response;
}

/** carrierLogo serves imported public images without license or database I/O. */
export async function carrierLogo(
  request: Request,
  bucket: R2Bucket,
  plmn: string,
): Promise<Response> {
  const key = /^[0-9]{5,6}$/.test(plmn) ? keys.get(plmn) : undefined;
  if (!key) {
    return missingLogo();
  }

  try {
    const condition = new Headers();
    const ifNoneMatch = request.headers.get('if-none-match');
    if (ifNoneMatch) {
      condition.set('if-none-match', ifNoneMatch);
    }
    const object =
      request.method === 'HEAD'
        ? await bucket.head(key)
        : await bucket.get(key, { onlyIf: condition });
    if (!object) {
      return missingLogo();
    }
    const body =
      'body' in object && object.body instanceof ReadableStream
        ? object.body
        : null;

    const headers = new Headers({
      'content-type': 'image/png',
      'cache-control': 'public, max-age=86400',
      'x-content-type-options': 'nosniff',
      'access-control-allow-origin': '*',
      etag: object.httpEtag,
    });
    // Conditional R2 reads return metadata without opening a body stream.
    const notModified =
      request.method === 'HEAD'
        ? (ifNoneMatch?.split(',') ?? []).some((tag) => {
            const normalized = tag.trim().replace(/^W\//, '');
            return normalized === '*' || normalized === object.httpEtag;
          })
        : body === null;
    if (notModified) {
      return new Response(null, { status: 304, headers });
    }
    headers.set('content-length', String(object.size));
    return new Response(body, { headers });
  } catch (caught) {
    logError('read carrier logo', caught, { plmn });
    return apiError(
      'service_unavailable',
      'carrier logo storage unavailable',
      503,
    );
  }
}
