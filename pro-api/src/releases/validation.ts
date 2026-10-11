import type { Manifest, ReleaseChannel, DownloadTicket } from './types';
import {
  isRecord,
  isNonEmptyString,
  isSafeInteger,
  isRfc3339,
} from '../http/validation';

export function manifest(value: unknown): Manifest | null {
  if (
    !isRecord(value) ||
    value.schemaVersion !== 1 ||
    value.edition !== 'pro' ||
    (value.channel !== 'stable' && value.channel !== 'dev') ||
    !isNonEmptyString(value.version) ||
    !isNonEmptyString(value.commit) ||
    !/^[0-9a-f]{40}$/.test(value.commit) ||
    !isRfc3339(value.publishedAt) ||
    typeof value.notes !== 'string' ||
    !Array.isArray(value.artifacts)
  ) {
    return null;
  }
  if (
    (value.channel === 'stable' &&
      !/^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(
        value.version,
      )) ||
    (value.channel === 'dev' &&
      value.version.toLowerCase() !== `dev-${value.commit.slice(0, 8)}`)
  ) {
    return null;
  }
  const artifacts: Manifest['artifacts'] = [];
  const targets = new Set<string>();
  for (const candidate of value.artifacts) {
    if (
      !isRecord(candidate) ||
      !isNonEmptyString(candidate.target) ||
      candidate.target.trim() !== candidate.target ||
      targets.has(candidate.target) ||
      !isNonEmptyString(candidate.name) ||
      !/^[A-Za-z0-9._-]+$/.test(candidate.name) ||
      candidate.compression !== 'gzip' ||
      !candidate.name.endsWith('.gz') ||
      !isSafeInteger(candidate.size) ||
      candidate.size < 1 ||
      typeof candidate.sha256 !== 'string' ||
      !/^[0-9a-f]{64}$/.test(candidate.sha256) ||
      !isSafeInteger(candidate.executableSize) ||
      candidate.executableSize < 1 ||
      typeof candidate.executableSha256 !== 'string' ||
      !/^[0-9a-f]{64}$/.test(candidate.executableSha256)
    ) {
      return null;
    }
    targets.add(candidate.target);
    artifacts.push({
      target: candidate.target,
      name: candidate.name,
      compression: 'gzip',
      size: candidate.size,
      sha256: candidate.sha256,
      executableSize: candidate.executableSize,
      executableSha256: candidate.executableSha256,
    });
  }
  if (artifacts.length === 0) {
    return null;
  }
  return {
    schemaVersion: 1,
    edition: 'pro',
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
    (value.channel !== 'stable' && value.channel !== 'dev') ||
    !isNonEmptyString(value.version) ||
    !isNonEmptyString(value.target) ||
    !isNonEmptyString(value.path) ||
    !isSafeInteger(value.expiresAt)
  ) {
    return null;
  }
  const expectedPrefix = `${value.channel}/versions/${value.version}/`;
  if (!value.path.startsWith(expectedPrefix)) {
    return null;
  }

  const channel: ReleaseChannel = value.channel;
  const fields = {
    channel,
    version: value.version,
    target: value.target,
    path: value.path,
    expiresAt: value.expiresAt,
  };
  if (value.purpose === 'bootstrap') {
    if (
      !isNonEmptyString(value.jti) ||
      !isSafeInteger(value.telegramId) ||
      value.telegramId <= 0 ||
      value.deviceId !== undefined
    ) {
      return null;
    }
    return {
      purpose: 'bootstrap',
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
