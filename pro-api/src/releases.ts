import type { RequestContext } from "./context";
import { apiError, json } from "./http";
import { authorizeRequest, isActive } from "./license";
import { createTicket, parseTicket } from "./tickets";
import type { Manifest, ReleaseChannel } from "./types";
import { manifest as parseManifest } from "./validation";

const maxManifestSize = 1024 * 1024;
const maxSignatureSize = 8 * 1024;
const deviceTicketLifetimeSeconds = 5 * 60;
const bootstrapTicketLifetimeSeconds = 15 * 60;

type ReleaseSnapshot = {
  manifest: Manifest;
  text: string;
};

type ReleaseErrorCode =
  | "release_not_found"
  | "release_manifest_too_large"
  | "release_manifest_invalid";

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
  const manifestObject = await env.sigmo_pro_updates.get(
    `${channel}/latest/manifest.json`,
  );
  if (!manifestObject)
    throw new ReleaseMetadataError(
      "release_not_found",
      404,
      "release is unavailable",
    );
  if (manifestObject.size > maxManifestSize)
    throw new ReleaseMetadataError(
      "release_manifest_too_large",
      502,
      "release manifest exceeds size limit",
    );
  const text = await manifestObject.text();
  let manifest: Manifest | null;
  try {
    manifest = parseManifest(JSON.parse(text) as unknown);
  } catch {
    manifest = null;
  }
  if (!manifest || manifest.channel !== channel)
    throw new ReleaseMetadataError(
      "release_manifest_invalid",
      502,
      "release manifest is invalid",
    );
  return { manifest, text };
}

function releaseMetadataResponse(caught: unknown): Response | null {
  if (!(caught instanceof ReleaseMetadataError)) return null;
  return apiError(caught.errorCode, caught.message, caught.statusCode);
}

export type BootstrapDownload = {
  target: string;
  url: string;
};

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
        | "entitlement_inactive"
        | "release_unavailable"
        | "target_unavailable";
      target?: string;
    };

type BootstrapDownloadInput = {
  telegramId: number;
  channel: ReleaseChannel;
  targets: readonly string[];
};

export async function createBootstrapDownloads(
  request: Request,
  context: RequestContext,
  input: BootstrapDownloadInput,
): Promise<BootstrapDownloadResult> {
  const entitlement = await context.db.findEntitlement(input.telegramId);
  if (!isActive(entitlement))
    return { ok: false, reason: "entitlement_inactive" };

  let release: ReleaseSnapshot;
  try {
    release = await loadLatestManifest(context.env, input.channel);
  } catch (caught) {
    if (caught instanceof ReleaseMetadataError)
      return { ok: false, reason: "release_unavailable" };
    throw caught;
  }

  const artifacts = input.targets.map((target) => ({
    target,
    artifact: release.manifest.artifacts.find(
      (candidate) => candidate.target === target,
    ),
  }));
  const unavailable = artifacts.find(({ artifact }) => !artifact);
  if (unavailable)
    return {
      ok: false,
      reason: "target_unavailable",
      target: unavailable.target,
    };

  const expiresAt =
    Math.floor(Date.now() / 1000) + bootstrapTicketLifetimeSeconds;
  const downloads: BootstrapDownload[] = [];
  for (const { target, artifact } of artifacts) {
    if (!artifact) continue;
    const ticket = await createTicket(
      context.env.SIGMO_DOWNLOAD_TICKET_SECRET,
      {
        purpose: "bootstrap",
        telegramId: input.telegramId,
        channel: input.channel,
        version: release.manifest.version,
        target,
        path: `${input.channel}/versions/${release.manifest.version}/${artifact.name}`,
        expiresAt,
      },
    );
    const downloadURL = new URL(`/v1/downloads/${ticket}`, request.url);
    downloadURL.protocol = "https:";
    downloads.push({
      target,
      url: downloadURL.toString(),
    });
  }
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
  if (channel !== "stable" && channel !== "dev")
    return apiError(
      "release_channel_not_found",
      "unknown release channel",
      404,
    );
  const authorization = await authorizeRequest(request, context);
  if (!authorization.ok) return authorization.response;
  const { lease } = authorization;
  const target = new URL(request.url).searchParams.get("target") ?? "";
  let release: ReleaseSnapshot;
  try {
    release = await loadLatestManifest(env, channel);
  } catch (caught) {
    const response = releaseMetadataResponse(caught);
    if (response) return response;
    throw caught;
  }
  const { manifest, text: manifestText } = release;
  const signatureObject = await env.sigmo_pro_updates.get(
    `${channel}/versions/${manifest.version}/manifest.json.sig`,
  );
  if (!signatureObject)
    return apiError(
      "release_signature_not_found",
      "release signature is unavailable",
      404,
    );
  if (signatureObject.size > maxSignatureSize)
    return apiError(
      "release_signature_too_large",
      "release signature exceeds size limit",
      502,
    );
  const signature = (await signatureObject.text()).trim();
  if (!signature)
    return apiError(
      "release_signature_invalid",
      "release signature is invalid",
      502,
    );
  const artifact = manifest.artifacts.find(
    (candidate) => candidate.target === target,
  );
  if (!artifact)
    return apiError("release_target_not_found", "target is unavailable", 404);
  const ticket = await createTicket(env.SIGMO_DOWNLOAD_TICKET_SECRET, {
    deviceId: lease.deviceId,
    channel,
    version: manifest.version,
    target,
    path: `${channel}/versions/${manifest.version}/${artifact.name}`,
    expiresAt: Math.floor(Date.now() / 1000) + deviceTicketLifetimeSeconds,
  });
  const downloadURL = new URL(
    `/v1/downloads/${ticket}`,
    request.url,
  ).toString();
  return json({
    manifest: manifestText,
    signature,
    downloadUrl: downloadURL,
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
  if (!ticket)
    return apiError(
      "download_ticket_invalid",
      "download ticket is invalid or expired",
      403,
    );
  if ("purpose" in ticket) {
    const entitlement = await context.db.findEntitlement(ticket.telegramId);
    if (!isActive(entitlement))
      return apiError(
        "license_entitlement_inactive",
        "authorization revoked or expired",
        403,
      );
  } else {
    const authorization = await authorizeRequest(request, context);
    if (!authorization.ok) return authorization.response;
    if (ticket.deviceId !== authorization.lease.deviceId)
      return apiError(
        "download_ticket_invalid",
        "download ticket is invalid or expired",
        403,
      );
  }
  const rangeRequested = request.headers.has("range");
  const object = rangeRequested
    ? await env.sigmo_pro_updates.get(ticket.path, { range: request.headers })
    : await env.sigmo_pro_updates.get(ticket.path);
  if (!object)
    return apiError("release_artifact_not_found", "artifact not found", 404);

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set("cache-control", "private, no-store");
  headers.delete("content-encoding");
  headers.set("content-type", "application/gzip");
  headers.set("x-content-type-options", "nosniff");
  headers.set("etag", object.httpEtag);
  headers.set("accept-ranges", "bytes");
  const name = ticket.path.slice(ticket.path.lastIndexOf("/") + 1);
  headers.set("content-disposition", `attachment; filename="${name}"`);
  if (rangeRequested && object.range) {
    let offset: number;
    let length: number;
    if ("suffix" in object.range && typeof object.range.suffix === "number") {
      length = Math.min(object.range.suffix, object.size);
      offset = object.size - length;
    } else {
      offset =
        "offset" in object.range && typeof object.range.offset === "number"
          ? object.range.offset
          : 0;
      length =
        "length" in object.range && typeof object.range.length === "number"
          ? object.range.length
          : object.size - offset;
    }
    headers.set(
      "content-range",
      `bytes ${offset}-${offset + length - 1}/${object.size}`,
    );
    headers.set("content-length", String(length));
    return new Response(object.body, { status: 206, headers });
  }
  headers.set("content-length", String(object.size));
  return new Response(object.body, { headers });
}
