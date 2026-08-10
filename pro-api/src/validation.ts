import type {
  DownloadTicket,
  Lease,
  Manifest,
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
): { deviceId: string; publicKey: string } | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.publicKey)
  ) {
    return null;
  }
  return { deviceId: value.deviceId, publicKey: value.publicKey };
}

export function challengeRequest(value: unknown): { deviceId: string } | null {
  if (!isRecord(value) || !isNonEmptyString(value.deviceId)) return null;
  return { deviceId: value.deviceId };
}

export function leaseRequest(value: unknown): {
  deviceId: string;
  challenge: string;
  signature: string;
} | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.deviceId) ||
    !isNonEmptyString(value.challenge) ||
    !isNonEmptyString(value.signature)
  ) {
    return null;
  }
  return {
    deviceId: value.deviceId,
    challenge: value.challenge,
    signature: value.signature,
  };
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
      !isSafeInteger(candidate.size) ||
      candidate.size < 1 ||
      typeof candidate.sha256 !== "string" ||
      !/^[0-9a-f]{64}$/.test(candidate.sha256)
    ) {
      return null;
    }
    targets.add(candidate.target);
    artifacts.push({
      target: candidate.target,
      name: candidate.name,
      size: candidate.size,
      sha256: candidate.sha256,
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
    !isNonEmptyString(value.deviceId) ||
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
  return {
    deviceId: value.deviceId,
    channel: value.channel,
    version: value.version,
    target: value.target,
    path: value.path,
    expiresAt: value.expiresAt,
  };
}
