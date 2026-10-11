export function bearerToken(request: Request): string {
  const authorization = request.headers.get('authorization') ?? '';
  return /^Bearer +([A-Za-z0-9._~+/-]+=*)$/i.exec(authorization)?.[1] ?? '';
}
