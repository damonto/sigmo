import type { RequestContext } from "./context";
import { apiError, json } from "./http";
import { authorizeRequest } from "./license";
import { createTicket, parseTicket } from "./tickets";
import type { Manifest } from "./types";
import { manifest as parseManifest } from "./validation";

const maxManifestSize = 1024 * 1024;
const maxSignatureSize = 8 * 1024;

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
  const manifestObject = await env.sigmo_pro_updates.get(
    `${channel}/latest/manifest.json`,
  );
  if (!manifestObject)
    return apiError("release_not_found", "release is unavailable", 404);
  if (manifestObject.size > maxManifestSize)
    return apiError(
      "release_manifest_too_large",
      "release manifest exceeds size limit",
      502,
    );
  const manifestText = await manifestObject.text();
  let manifest: Manifest | null;
  try {
    manifest = parseManifest(JSON.parse(manifestText) as unknown);
  } catch {
    manifest = null;
  }
  if (!manifest || manifest.channel !== channel)
    return apiError(
      "release_manifest_invalid",
      "release manifest is invalid",
      502,
    );
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
    expiresAt: Math.floor(Date.now() / 1000) + 300,
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
  const authorization = await authorizeRequest(request, context);
  if (!authorization.ok) return authorization.response;
  const { lease } = authorization;
  const ticket = await parseTicket(
    env.SIGMO_DOWNLOAD_TICKET_SECRET,
    ticketValue,
  );
  if (!ticket || ticket.deviceId !== lease.deviceId)
    return apiError(
      "download_ticket_invalid",
      "download ticket is invalid or expired",
      403,
    );
  const object = await env.sigmo_pro_updates.get(ticket.path, {
    range: request.headers,
  });
  if (!object)
    return apiError("release_artifact_not_found", "artifact not found", 404);

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set("cache-control", "private, no-store");
  headers.set("content-type", "application/octet-stream");
  headers.set("x-content-type-options", "nosniff");
  headers.set("etag", object.httpEtag);
  headers.set("accept-ranges", "bytes");
  const name = ticket.path.slice(ticket.path.lastIndexOf("/") + 1);
  headers.set("content-disposition", `attachment; filename="${name}"`);
  if (object.range) {
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
