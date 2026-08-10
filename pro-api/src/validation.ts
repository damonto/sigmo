import type {
  DownloadTicket,
  Lease,
  Manifest,
  ReleaseChannel,
  SignedLease,
  TelegramUpdate,
} from "./types";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function isRFC3339(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(
      value,
    );
  if (!match) return false;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const hour = Number(match[4]);
  const minute = Number(match[5]);
  const second = Number(match[6]);
  const offsetHour = match[7] === undefined ? 0 : Number(match[7]);
  const offsetMinute = match[8] === undefined ? 0 : Number(match[8]);
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return (
    month >= 1 &&
    month <= 12 &&
    day >= 1 &&
    day <= (daysInMonth[month - 1] ?? 0) &&
    hour <= 23 &&
    minute <= 59 &&
    second <= 59 &&
    offsetHour <= 23 &&
    offsetMinute <= 59 &&
    Number.isFinite(Date.parse(value))
  );
}

function isSafeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value);
}

export function pairingRequest(
  value: unknown,
): {
  deviceId: string;
  publicKey: string;
  refreshTokenHash: string;
  fingerprintHash: string;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.publicKey) ||
    !isSHA256(value.refreshTokenHash) ||
    !isSHA256(value.fingerprintHash)
  ) {
    return null;
  }
  return {
    deviceId: value.deviceId,
    publicKey: value.publicKey,
    refreshTokenHash: value.refreshTokenHash,
    fingerprintHash: value.fingerprintHash,
  };
}

export function challengeRequest(value: unknown): {
  deviceId: string;
  sessionId: string;
  generation: number;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.sessionId) ||
    !isSafeInteger(value.generation) ||
    value.generation < 1
  )
    return null;
  return {
    deviceId: value.deviceId,
    sessionId: value.sessionId,
    generation: value.generation,
  };
}

export function leaseRequest(value: unknown): {
  deviceId: string;
  sessionId: string;
  generation: number;
  challenge: string;
  refreshToken: string;
  nextRefreshTokenHash: string;
  rotationId: string;
  fingerprintHash: string;
  signature: string;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.sessionId) ||
    !isSafeInteger(value.generation) ||
    value.generation < 1 ||
    !isNonEmptyString(value.challenge) ||
    !isNonEmptyString(value.refreshToken) ||
    !isSHA256(value.nextRefreshTokenHash) ||
    !isNonEmptyString(value.rotationId) ||
    !isSHA256(value.fingerprintHash) ||
    !isNonEmptyString(value.signature)
  ) {
    return null;
  }
  return {
    deviceId: value.deviceId,
    sessionId: value.sessionId,
    generation: value.generation,
    challenge: value.challenge,
    refreshToken: value.refreshToken,
    nextRefreshTokenHash: value.nextRefreshTokenHash,
    rotationId: value.rotationId,
    fingerprintHash: value.fingerprintHash,
    signature: value.signature,
  };
}

function isSHA256(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9_-]{43}$/.test(value);
}

export function telegramUpdate(value: unknown): TelegramUpdate | null {
  if (!isRecord(value)) return null;
  if (value.message === undefined) return {};
  if (!isRecord(value.message) || !isRecord(value.message.chat)) return null;
  const chatType = value.message.chat.type;
  if (
    !isSafeInteger(value.message.chat.id) ||
    (chatType !== "private" &&
      chatType !== "group" &&
      chatType !== "supergroup" &&
      chatType !== "channel")
  )
    return null;
  const chat: NonNullable<TelegramUpdate["message"]>["chat"] = {
    id: value.message.chat.id,
    type: chatType,
  };
  if (value.message.from === undefined || value.message.text === undefined) {
    return { message: { chat } };
  }
  if (
    !isRecord(value.message.from) ||
    !isSafeInteger(value.message.from.id) ||
    value.message.from.id <= 0 ||
    typeof value.message.from.first_name !== "string" ||
    typeof value.message.text !== "string" ||
    (value.message.from.last_name !== undefined &&
      typeof value.message.from.last_name !== "string") ||
    (value.message.from.username !== undefined &&
      typeof value.message.from.username !== "string")
  ) {
    return null;
  }
  return {
    message: {
      chat,
      from: {
        id: value.message.from.id,
        first_name: value.message.from.first_name,
        last_name: value.message.from.last_name,
        username: value.message.from.username,
      },
      text: value.message.text,
    },
  };
}

function isLease(value: unknown): value is Lease {
  return (
    isRecord(value) &&
    value.schemaVersion === 1 &&
    isNonEmptyString(value.deviceId) &&
    isNonEmptyString(value.sessionId) &&
    isSafeInteger(value.generation) &&
    value.generation > 0 &&
    isSafeInteger(value.telegramId) &&
    value.telegramId > 0 &&
    value.status === "active" &&
    typeof value.displayName === "string" &&
    (value.username === undefined || typeof value.username === "string") &&
    isRFC3339(value.issuedAt) &&
    isRFC3339(value.refreshAfter) &&
    isRFC3339(value.expiresAt) &&
    (value.entitlementExpiresAt === undefined ||
      isRFC3339(value.entitlementExpiresAt))
  );
}

export function signedLease(value: unknown): SignedLease | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.signature) ||
    !isLease(value.lease)
  ) {
    return null;
  }
  return { lease: value.lease, signature: value.signature };
}

export function manifest(value: unknown): Manifest | null {
  if (
    !isRecord(value) ||
    value.schemaVersion !== 1 ||
    value.edition !== "pro" ||
    (value.channel !== "stable" && value.channel !== "dev") ||
    !isNonEmptyString(value.version) ||
    !isNonEmptyString(value.commit) ||
    !/^[0-9a-f]{40}$/.test(value.commit) ||
    !isRFC3339(value.publishedAt) ||
    typeof value.notes !== "string" ||
    !Array.isArray(value.artifacts)
  ) {
    return null;
  }
  if (
    (value.channel === "stable" &&
      !/^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(
        value.version,
      )) ||
    (value.channel === "dev" &&
      value.version.toLowerCase() !== `dev-${value.commit.slice(0, 8)}`)
  ) {
    return null;
  }
  const artifacts: Manifest["artifacts"] = [];
  const targets = new Set<string>();
  for (const candidate of value.artifacts) {
    if (
      !isRecord(candidate) ||
      !isNonEmptyString(candidate.target) ||
      candidate.target.trim() !== candidate.target ||
      targets.has(candidate.target) ||
      !isNonEmptyString(candidate.name) ||
      !/^[A-Za-z0-9._-]+$/.test(candidate.name) ||
      candidate.compression !== "gzip" ||
      !candidate.name.endsWith(".gz") ||
      !isSafeInteger(candidate.size) ||
      candidate.size < 1 ||
      typeof candidate.sha256 !== "string" ||
      !/^[0-9a-f]{64}$/.test(candidate.sha256) ||
      !isSafeInteger(candidate.executableSize) ||
      candidate.executableSize < 1 ||
      typeof candidate.executableSha256 !== "string" ||
      !/^[0-9a-f]{64}$/.test(candidate.executableSha256)
    ) {
      return null;
    }
    targets.add(candidate.target);
    artifacts.push({
      target: candidate.target,
      name: candidate.name,
      compression: "gzip",
      size: candidate.size,
      sha256: candidate.sha256,
      executableSize: candidate.executableSize,
      executableSha256: candidate.executableSha256,
    });
  }
  if (artifacts.length === 0) return null;
  return {
    schemaVersion: 1,
    edition: "pro",
    channel: value.channel,
    version: value.version,
    commit: value.commit,
    publishedAt: value.publishedAt,
    notes: value.notes,
    artifacts,
  };
}

export function downloadTicket(value: unknown): DownloadTicket | null {
  if (
    !isRecord(value) ||
    (value.channel !== "stable" && value.channel !== "dev") ||
    !isNonEmptyString(value.version) ||
    !isNonEmptyString(value.target) ||
    !isNonEmptyString(value.path) ||
    !isSafeInteger(value.expiresAt)
  ) {
    return null;
  }
  const expectedPrefix = `${value.channel}/versions/${value.version}/`;
  if (!value.path.startsWith(expectedPrefix)) return null;

  const channel: ReleaseChannel = value.channel;
  const fields = {
    channel,
    version: value.version,
    target: value.target,
    path: value.path,
    expiresAt: value.expiresAt,
  };
  if (value.purpose === "bootstrap") {
    if (
      !isNonEmptyString(value.jti) ||
      !isSafeInteger(value.telegramId) ||
      value.telegramId <= 0 ||
      value.deviceId !== undefined
    ) {
      return null;
    }
    return {
      purpose: "bootstrap",
      jti: value.jti,
      telegramId: value.telegramId,
      ...fields,
    };
  }
  if (
    !isNonEmptyString(value.deviceId) ||
    value.purpose !== undefined ||
    value.jti !== undefined ||
    value.telegramId !== undefined
  ) {
    return null;
  }
  return { deviceId: value.deviceId, ...fields };
}
