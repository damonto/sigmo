import type { ProblemCode } from './response';

export const MAX_JSON_BODY_SIZE = 64 * 1024;

export class RequestBodyError extends Error {
  constructor(
    readonly code: ProblemCode,
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

/** readJson limits actual bytes; callers assign request or upstream semantics. */
export async function readJson(
  body: ReadableStream<Uint8Array> | null,
): Promise<unknown> {
  if (!body) {
    throw new Error('JSON body is required');
  }
  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      size += value.byteLength;
      if (size > MAX_JSON_BODY_SIZE) {
        await reader.cancel().catch(() => {
          // Cleanup must not replace the size-limit error with a stream error.
        });
        throw new RequestBodyError(
          'request_body_too_large',
          413,
          'request body exceeds size limit',
        );
      }
      chunks.push(value);
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
  return JSON.parse(
    new TextDecoder('utf-8', { fatal: true, ignoreBOM: false }).decode(data),
  ) as unknown;
}

export async function parseJson(request: Request): Promise<unknown> {
  const lengthHeader = request.headers.get('content-length');
  if (lengthHeader !== null) {
    const length = Number(lengthHeader);
    if (!/^\d+$/.test(lengthHeader) || !Number.isSafeInteger(length)) {
      throw new RequestBodyError(
        'request_body_invalid',
        400,
        'invalid content length',
      );
    }
    if (length > MAX_JSON_BODY_SIZE) {
      throw new RequestBodyError(
        'request_body_too_large',
        413,
        'request body exceeds size limit',
      );
    }
  }
  if (!request.body) {
    throw new RequestBodyError(
      'request_body_invalid',
      400,
      'JSON body is required',
    );
  }
  if (
    request.headers.get('content-type')?.split(';')[0].trim().toLowerCase() !==
    'application/json'
  ) {
    throw new RequestBodyError(
      'unsupported_media_type',
      415,
      'request requires application/json',
    );
  }
  try {
    return await readJson(request.body);
  } catch (caught) {
    if (caught instanceof RequestBodyError) {
      throw caught;
    }
    throw new RequestBodyError(
      'request_body_invalid',
      400,
      'invalid JSON body',
    );
  }
}
