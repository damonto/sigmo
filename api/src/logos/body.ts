/** readLimitedBody buffers a Simdex response only after enforcing a size cap. */
export async function readLimitedBody(
  response: Response,
  limit: number,
  label: string,
): Promise<Uint8Array> {
  const contentLength = Number(response.headers.get('content-length') ?? '0');
  if (contentLength > limit) {
    throw new Error(`${label} exceeds size limit`);
  }
  const bytes = new Uint8Array(await response.arrayBuffer());
  if (bytes.byteLength > limit) {
    throw new Error(`${label} exceeds size limit`);
  }
  return bytes;
}
