import catalog from '../../../../internal/pkg/apierror/catalog.json'

/** problemResponse creates the same wire representation as the two servers. */
export function problemResponse(
  status: number,
  code: keyof typeof catalog,
  detail: string,
  requestID = 'req-1',
): Response {
  return Response.json(
    {
      type: `https://sigmo-pro.952007.xyz/problems/${code}`,
      title: catalog[code],
      status,
      detail,
      error_code: code,
      request_id: requestID,
    },
    { status, headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': requestID } },
  )
}
