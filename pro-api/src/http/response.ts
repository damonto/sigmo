import catalog from '../../../internal/pkg/apierror/catalog.json';

export const PROBLEM_MEDIA_TYPE = 'application/problem+json';
export const PROBLEM_TYPE_PREFIX = 'https://sigmo-pro.952007.xyz/problems/';
export type ProblemCode = keyof typeof catalog;

const JSON_HEADERS = {
  'cache-control': 'no-store',
  'content-type': 'application/json; charset=utf-8',
  'x-content-type-options': 'nosniff',
};

export function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), { status, headers: JSON_HEADERS });
}

// Retain structured problems until the HTTP boundary assigns its request ID.
// Download streams never pass through JSON parsing or buffering.
class ProblemResponse extends Response {
  constructor(
    readonly code: ProblemCode,
    readonly detail: string,
    status: number,
  ) {
    const requestId = crypto.randomUUID();
    super(problemBody(code, detail, status, requestId), {
      status,
      headers: {
        ...JSON_HEADERS,
        'content-type': PROBLEM_MEDIA_TYPE,
        'x-request-id': requestId,
      },
    });
  }
}

function problemBody(
  code: ProblemCode,
  detail: string,
  status: number,
  requestId: string,
): string {
  return JSON.stringify({
    type: PROBLEM_TYPE_PREFIX + code,
    title: catalog[code],
    status,
    detail,
    error_code: code,
    request_id: requestId,
  });
}

export function apiError(
  code: ProblemCode,
  detail: string,
  status: number,
): Response {
  return new ProblemResponse(code, detail, status);
}

/** finishResponse binds the same ID to the body, headers, and request log. */
export function finishResponse(
  response: Response,
  requestId: string,
): Response {
  response.headers.set('x-request-id', requestId);
  if (response instanceof ProblemResponse) {
    return new Response(
      problemBody(response.code, response.detail, response.status, requestId),
      response,
    );
  }
  return response;
}

export function methodNotAllowed(...allowed: string[]): Response {
  const response = apiError(
    'method_not_allowed',
    `This resource supports ${allowed.join(', ')}.`,
    405,
  );
  response.headers.set('allow', allowed.join(', '));
  return response;
}

/** problemDescription serves immutable protocol metadata without business I/O. */
export function problemDescription(code: string): Response {
  if (!Object.hasOwn(catalog, code)) {
    return apiError('resource_not_found', 'problem type not found', 404);
  }
  // hasOwn checks this untrusted route parameter against the checked-in catalog.
  const title = catalog[code as ProblemCode];
  return json({
    type: PROBLEM_TYPE_PREFIX + code,
    title,
    error_code: code,
    description:
      'Use the HTTP status and registered problem type to interpret this failure. Detail and title are for display only.',
    documentation:
      'https://github.com/damonto/sigmo/blob/main/docs/http-api-contract.md',
  });
}
