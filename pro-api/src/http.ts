const maxJSONBodySize = 64 * 1024;

export const requestBodyTooLarge = new Error(
  "request body exceeds size limit",
);

const jsonHeaders = {
  "cache-control": "no-store",
  "content-type": "application/json; charset=utf-8",
  "x-content-type-options": "nosniff",
};

export function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), { status, headers: jsonHeaders });
}

export function apiError(
  errorCode: string,
  message: string,
  status: number,
): Response {
  return json({ error_code: errorCode, message }, status);
}

export function methodNotAllowed(...allowed: string[]): Response {
  const response = apiError(
    "method_not_allowed",
    "method not allowed",
    405,
  );
  response.headers.set("allow", allowed.join(", "));
  return response;
}

export function logError(
  message: string,
  caught: unknown,
  details: Record<string, string | number> = {},
): void {
  console.error(
    JSON.stringify({
      message,
      error: caught instanceof Error ? caught.message : String(caught),
      ...details,
    }),
  );
}

export function nowISO(): string {
  return new Date().toISOString();
}

export async function parseJSON(request: Request): Promise<unknown | null> {
  const contentLength = request.headers.get("content-length");
  if (contentLength !== null) {
    const length = Number(contentLength);
    if (!Number.isSafeInteger(length) || length < 0) return null;
    if (length > maxJSONBodySize) throw requestBodyTooLarge;
  }
  if (!request.body) return null;

  const reader = request.body.getReader();
  const chunks: Uint8Array<ArrayBuffer>[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxJSONBodySize) {
        await reader.cancel();
        throw requestBodyTooLarge;
      }
      chunks.push(value.slice());
    }
  } finally {
    reader.releaseLock();
  }

  const data = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    data.set(chunk, offset);
    offset += chunk.byteLength;
  }
  try {
    return JSON.parse(new TextDecoder().decode(data)) as unknown;
  } catch {
    return null;
  }
}
