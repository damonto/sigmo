import type { RequestContext } from '../http/context';
import { randomToken, TEXT_ENCODER, verifyEd25519 } from '../security/crypto';
import { apiError, json } from '../http/response';
import { authorizeRequest, isActive } from '../license/service';
import { createTicket, parseTicket } from './tickets';
import type { Manifest, ReleaseChannel } from './types';
import { manifest as parseManifest } from './validation';

const MAX_MANIFEST_SIZE = 1024 * 1024;
const MAX_SIGNATURE_SIZE = 8 * 1024;
const DEVICE_TICKET_LIFETIME_SECONDS = 5 * 60;
const BOOTSTRAP_TICKET_LIFETIME_SECONDS = 5 * 60;

interface ReleaseSnapshot {
  manifest: Manifest;
  text: string;
  signature: string;
}

type ReleaseErrorCode =
  | 'release_not_found'
  | 'release_manifest_too_large'
  | 'release_manifest_invalid'
  | 'release_signature_not_found'
  | 'release_signature_too_large'
  | 'release_signature_invalid';

class ReleaseMetadataError extends Error {
  constructor(
    readonly errorCode: ReleaseErrorCode,
    readonly statusCode: number,
    message: string,
  ) {
    super(message);
  }
}

async function loadLatestManifest(
  env: Env,
  channel: ReleaseChannel,
): Promise<ReleaseSnapshot> {
  const manifestObject = await env.BUCKET.get(
    `${channel}/latest/manifest.json`,
  );
  if (!manifestObject) {
    throw new ReleaseMetadataError(
      'release_not_found',
      404,
      'release is unavailable',
    );
  }
  if (manifestObject.size > MAX_MANIFEST_SIZE) {
    await manifestObject.body.cancel();
    throw new ReleaseMetadataError(
      'release_manifest_too_large',
      502,
      'release manifest exceeds size limit',
    );
  }
  const text = await manifestObject.text();
  let manifest: Manifest | null;
  try {
    manifest = parseManifest(JSON.parse(text) as unknown);
  } catch {
    manifest = null;
  }
  if (!manifest || manifest.channel !== channel) {
    throw new ReleaseMetadataError(
      'release_manifest_invalid',
      502,
      'release manifest is invalid',
    );
  }
  const signatureObject = await env.BUCKET.get(
    `${channel}/versions/${manifest.version}/manifest.json.sig`,
  );
  if (!signatureObject) {
    throw new ReleaseMetadataError(
      'release_signature_not_found',
      404,
      'release signature is unavailable',
    );
  }
  if (signatureObject.size > MAX_SIGNATURE_SIZE) {
    await signatureObject.body.cancel();
    throw new ReleaseMetadataError(
      'release_signature_too_large',
      502,
      'release signature exceeds size limit',
    );
  }
  const signature = (await signatureObject.text()).trim();
  if (
    !signature ||
    !(await verifyEd25519(
      env.SIGMO_RELEASE_PUBLIC_KEY,
      TEXT_ENCODER.encode(text),
      signature,
    ))
  ) {
    throw new ReleaseMetadataError(
      'release_signature_invalid',
      502,
      'release signature is invalid',
    );
  }
  return { manifest, text, signature };
}

function releaseMetadataResponse(caught: unknown): Response | null {
  if (!(caught instanceof ReleaseMetadataError)) {
    return null;
  }
  return apiError(caught.errorCode, caught.message, caught.statusCode);
}

export interface BootstrapDownload {
  target: string;
  url: string;
}

export type BootstrapDownloadResult =
  | {
      ok: true;
      channel: ReleaseChannel;
      version: string;
      downloads: BootstrapDownload[];
    }
  | {
      ok: false;
      reason:
        'entitlement_inactive' | 'release_unavailable' | 'target_unavailable';
      target?: string;
    };

interface BootstrapDownloadInput {
  telegramId: number;
  channel: ReleaseChannel;
  targets: readonly string[];
}

export async function createBootstrapDownloads(
  request: Request,
  context: RequestContext,
  input: BootstrapDownloadInput,
): Promise<BootstrapDownloadResult> {
  const entitlement = await context.db.findEntitlement(input.telegramId);
  if (!isActive(entitlement)) {
    return { ok: false, reason: 'entitlement_inactive' };
  }

  let release: ReleaseSnapshot;
  try {
    release = await loadLatestManifest(context.env, input.channel);
  } catch (caught) {
    if (caught instanceof ReleaseMetadataError) {
      return { ok: false, reason: 'release_unavailable' };
    }
    throw caught;
  }

  const artifacts = input.targets.map((target) => ({
    target,
    artifact: release.manifest.artifacts.find(
      (candidate) => candidate.target === target,
    ),
  }));
  const unavailable = artifacts.find(({ artifact }) => !artifact);
  if (unavailable) {
    return {
      ok: false,
      reason: 'target_unavailable',
      target: unavailable.target,
    };
  }

  const expiresAt =
    Math.floor(Date.now() / 1000) + BOOTSTRAP_TICKET_LIFETIME_SECONDS;
  const expiresAtIso = new Date(expiresAt * 1000).toISOString();
  const downloads: BootstrapDownload[] = [];
  const grants: Array<{
    jti: string;
    telegramId: number;
    objectPath: string;
    expiresAt: string;
  }> = [];
  for (const { target, artifact } of artifacts) {
    if (!artifact) {
      continue;
    }
    const jti = randomToken(24);
    const path = `${input.channel}/versions/${release.manifest.version}/${artifact.name}`;
    const ticket = await createTicket(
      context.env.SIGMO_DOWNLOAD_TICKET_SECRET,
      {
        purpose: 'bootstrap',
        jti,
        telegramId: input.telegramId,
        channel: input.channel,
        version: release.manifest.version,
        target,
        path,
        expiresAt,
      },
    );
    const downloadUrl = new URL(`/v1/downloads/${ticket}`, request.url);
    downloadUrl.protocol = 'https:';
    downloads.push({
      target,
      url: downloadUrl.toString(),
    });
    grants.push({
      jti,
      telegramId: input.telegramId,
      objectPath: path,
      expiresAt: expiresAtIso,
    });
  }
  await context.db.insertDownloadGrants(grants);
  return {
    ok: true,
    channel: input.channel,
    version: release.manifest.version,
    downloads,
  };
}

export async function latestRelease(
  request: Request,
  context: RequestContext,
  channel: string,
): Promise<Response> {
  const { env } = context;
  if (channel !== 'stable' && channel !== 'dev') {
    return apiError(
      'release_channel_not_found',
      'unknown release channel',
      404,
    );
  }
  const authorization = await authorizeRequest(request, context);
  if (!authorization.ok) {
    return authorization.response;
  }
  const { lease } = authorization;
  const target = new URL(request.url).searchParams.get('target') ?? '';
  let release: ReleaseSnapshot;
  try {
    release = await loadLatestManifest(env, channel);
  } catch (caught) {
    const response = releaseMetadataResponse(caught);
    if (response) {
      return response;
    }
    throw caught;
  }
  const { manifest, text: manifestText, signature } = release;
  const artifact = manifest.artifacts.find(
    (candidate) => candidate.target === target,
  );
  if (!artifact) {
    return apiError('release_target_not_found', 'target is unavailable', 404);
  }
  if (request.method === 'HEAD') {
    return json(null);
  }
  const ticket = await createTicket(env.SIGMO_DOWNLOAD_TICKET_SECRET, {
    deviceId: lease.deviceId,
    channel,
    version: manifest.version,
    target,
    path: `${channel}/versions/${manifest.version}/${artifact.name}`,
    expiresAt: Math.floor(Date.now() / 1000) + DEVICE_TICKET_LIFETIME_SECONDS,
  });
  const downloadUrl = new URL(
    `/v1/downloads/${ticket}`,
    request.url,
  ).toString();
  return json({
    manifest: manifestText,
    signature,
    downloadUrl: downloadUrl,
  });
}

export async function download(
  request: Request,
  context: RequestContext,
  ticketValue: string,
): Promise<Response> {
  const { env } = context;
  const ticket = await parseTicket(
    env.SIGMO_DOWNLOAD_TICKET_SECRET,
    ticketValue,
  );
  if (!ticket) {
    return apiError(
      'download_ticket_invalid',
      'download ticket is invalid or expired',
      403,
    );
  }
  if ('purpose' in ticket) {
    const entitlement = await context.db.findEntitlement(ticket.telegramId);
    if (!isActive(entitlement)) {
      return apiError(
        'license_entitlement_inactive',
        'authorization revoked or expired',
        403,
      );
    }
    const available =
      request.method === 'HEAD'
        ? await context.db.downloadGrantAvailable(
            ticket.jti,
            ticket.telegramId,
            ticket.path,
            new Date().toISOString(),
          )
        : await context.db.consumeDownloadGrant(
            ticket.jti,
            ticket.telegramId,
            ticket.path,
            new Date().toISOString(),
          );
    if (!available) {
      return apiError(
        'download_ticket_invalid',
        'download ticket is invalid or expired',
        403,
      );
    }
  } else {
    const authorization = await authorizeRequest(request, context);
    if (!authorization.ok) {
      return authorization.response;
    }
    if (ticket.deviceId !== authorization.lease.deviceId) {
      return apiError(
        'download_ticket_invalid',
        'download ticket is invalid or expired',
        403,
      );
    }
  }
  const bootstrap = 'purpose' in ticket;
  if (request.method === 'HEAD') {
    const object = await env.BUCKET.head(ticket.path);
    if (!object) {
      return apiError('release_artifact_not_found', 'artifact not found', 404);
    }
    return new Response(null, {
      headers: downloadHeaders(object, ticket.path, bootstrap),
    });
  }
  let range: { offset: number; length: number } | undefined;
  // Bootstrap grants authorize one full transfer. Ignoring Range avoids
  // consuming the grant on a partial representation that cannot be resumed.
  if (!bootstrap && request.headers.has('range')) {
    const metadata = await env.BUCKET.head(ticket.path);
    if (!metadata) {
      return apiError('release_artifact_not_found', 'artifact not found', 404);
    }
    const ifRange = request.headers.get('if-range');
    if (
      !ifRange ||
      ifRange === metadata.httpEtag ||
      (Number.isFinite(Date.parse(ifRange)) &&
        metadata.uploaded.getTime() <= Date.parse(ifRange))
    ) {
      const parsed = byteRange(
        request.headers.get('range') ?? '',
        metadata.size,
      );
      if (parsed === 'unsatisfiable') {
        const response = apiError(
          'range_not_satisfiable',
          'requested range does not overlap the artifact',
          416,
        );
        response.headers.set('content-range', `bytes */${metadata.size}`);
        response.headers.set('accept-ranges', 'bytes');
        response.headers.set('cache-control', 'private, no-store');
        return response;
      }
      range = parsed;
    }
  }
  const object = range
    ? await env.BUCKET.get(ticket.path, { range })
    : await env.BUCKET.get(ticket.path);
  if (!object) {
    return apiError('release_artifact_not_found', 'artifact not found', 404);
  }

  const headers = downloadHeaders(object, ticket.path, bootstrap);
  if (range && object.range) {
    const offset =
      'offset' in object.range && typeof object.range.offset === 'number'
        ? object.range.offset
        : range.offset;
    const length =
      'length' in object.range && typeof object.range.length === 'number'
        ? object.range.length
        : range.length;
    headers.set(
      'content-range',
      `bytes ${offset}-${offset + length - 1}/${object.size}`,
    );
    headers.set('content-length', String(length));
    return new Response(object.body, { status: 206, headers });
  }
  return new Response(object.body, { headers });
}

function downloadHeaders(
  object: R2Object,
  path: string,
  bootstrap: boolean,
): Headers {
  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set('cache-control', 'private, no-store');
  headers.delete('content-encoding');
  headers.set('content-type', 'application/gzip');
  headers.set('x-content-type-options', 'nosniff');
  headers.set('etag', object.httpEtag);
  headers.set('accept-ranges', bootstrap ? 'none' : 'bytes');
  const name = path.slice(path.lastIndexOf('/') + 1);
  headers.set('content-disposition', `attachment; filename="${name}"`);
  headers.set('content-length', String(object.size));
  return headers;
}

function byteRange(
  value: string,
  size: number,
): { offset: number; length: number } | 'unsatisfiable' | undefined {
  const match = /^bytes=(\d*)-(\d*)$/.exec(value.trim());
  // Multiple ranges and unsupported range units are optional; serve the full file.
  if (!match || (!match[1] && !match[2])) {
    return undefined;
  }
  if (!match[1]) {
    const length = Math.min(Number(match[2]), size);
    return length > 0 ? { offset: size - length, length } : 'unsatisfiable';
  }
  const offset = Number(match[1]);
  const last = match[2] ? Number(match[2]) : size - 1;
  if (match[2] && last < offset) {
    return undefined;
  }
  if (offset >= size) {
    return 'unsatisfiable';
  }
  return { offset, length: Math.min(last, size - 1) - offset + 1 };
}
