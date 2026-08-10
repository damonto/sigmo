export type Entitlement = {
  telegramId: number;
  status: "active" | "revoked";
  expiresAt: string | null;
  maxDevices: number;
  displayName: string;
  username: string;
};

export type Device = {
  deviceId: string;
  telegramId: number;
  publicKey: string;
  revokedAt: string | null;
};

export type DeviceSession = {
  deviceId: string;
  sessionId: string;
  generation: number;
  refreshTokenHash: string;
  previousRefreshTokenHash: string | null;
  lastRotationId: string | null;
  fingerprintHash: string;
};

export type ActiveEntitlement = {
  telegramId: number;
  expiresAt: string | null;
  maxDevices: number;
  displayName: string;
  username: string;
  activeDevices: number;
};

type Pairing = {
  pollTokenHash: string;
  deviceId: string;
  publicKey: string;
  sessionId: string;
  refreshTokenHash: string;
  fingerprintHash: string;
  telegramId: number | null;
  status: "pending" | "active" | "expired";
  expiresAt: string;
};

type Challenge = {
  expiresAt: string;
};

type DeviceSummary = {
  deviceId: string;
  lastSeenAt: string;
  revokedAt: string | null;
};

type TelegramCommandAdmin = {
  telegramId: number;
};

type PairingInsert = {
  pairingId: string;
  pollTokenHash: string;
  deviceId: string;
  publicKey: string;
  sessionId: string;
  refreshTokenHash: string;
  fingerprintHash: string;
  createdAt: string;
  expiresAt: string;
};

type ChallengeUpsert = {
  challengeHash: string;
  deviceId: string;
  createdAt: string;
  expiresAt: string;
};

type PairingActivation = {
  pairingId: string;
  deviceId: string;
  publicKey: string;
  sessionId: string;
  refreshTokenHash: string;
  fingerprintHash: string;
  telegramId: number;
  displayName: string;
  username: string;
  timestamp: string;
};

export type SessionRotation = {
  deviceId: string;
  sessionId: string;
  generation: number;
  refreshTokenHash: string;
  nextRefreshTokenHash: string;
  rotationId: string;
  fingerprintHash: string;
  challengeHash: string;
  timestamp: string;
};

type DownloadGrant = {
  jti: string;
  telegramId: number;
  objectPath: string;
  expiresAt: string;
};

type EntitlementGrant = {
  telegramId: number;
  expiresAt: string | null;
  maxDevices: number;
  timestamp: string;
};

const entitlementColumns = `
  telegram_id AS telegramId,
  status,
  expires_at AS expiresAt,
  max_devices AS maxDevices,
  display_name AS displayName,
  username`;

const deviceColumns = `
  device_id AS deviceId,
  telegram_id AS telegramId,
  public_key AS publicKey,
  revoked_at AS revokedAt`;

const pairingColumns = `
  poll_token_hash AS pollTokenHash,
  device_id AS deviceId,
  public_key AS publicKey,
  session_id AS sessionId,
  refresh_token_hash AS refreshTokenHash,
  fingerprint_hash AS fingerprintHash,
  telegram_id AS telegramId,
  status,
  expires_at AS expiresAt`;

const sessionColumns = `
  device_id AS deviceId,
  session_id AS sessionId,
  generation,
  refresh_token_hash AS refreshTokenHash,
  previous_refresh_token_hash AS previousRefreshTokenHash,
  last_rotation_id AS lastRotationId,
  fingerprint_hash AS fingerprintHash`;

export class Database {
  private readonly session: D1DatabaseSession;

  constructor(binding: D1Database) {
    // Authorization must observe current revocations. Anchor each request to
    // the primary, then keep subsequent reads and writes sequentially consistent.
    this.session = binding.withSession("first-primary");
  }

  async deleteExpiredState(timestamp: string): Promise<void> {
    await this.session.batch([
      this.session
        .prepare("DELETE FROM pairings WHERE expires_at <= ?")
        .bind(timestamp),
      this.session
        .prepare("DELETE FROM challenges WHERE expires_at <= ?")
        .bind(timestamp),
      this.session
        .prepare("DELETE FROM download_grants WHERE expires_at <= ?")
        .bind(timestamp),
    ]);
  }

  async insertPairing(
    pairing: PairingInsert,
    maxOutstanding: number,
  ): Promise<boolean> {
    const result = await this.session
      .prepare(
        `INSERT INTO pairings
           (pairing_id, poll_token_hash, device_id, public_key, session_id,
            refresh_token_hash, fingerprint_hash, telegram_id, status, created_at, expires_at)
         SELECT ?, ?, ?, ?, ?, ?, ?, NULL, 'pending', ?, ?
         WHERE (
           SELECT COUNT(*) FROM pairings
           WHERE device_id = ? AND status = 'pending' AND expires_at > ?
         ) < ?`,
      )
      .bind(
        pairing.pairingId,
        pairing.pollTokenHash,
        pairing.deviceId,
        pairing.publicKey,
        pairing.sessionId,
        pairing.refreshTokenHash,
        pairing.fingerprintHash,
        pairing.createdAt,
        pairing.expiresAt,
        pairing.deviceId,
        pairing.createdAt,
        maxOutstanding,
      )
      .run();
    return result.meta.changes === 1;
  }

  async findPairing(pairingId: string): Promise<Pairing | null> {
    return this.session
      .prepare(
        `SELECT ${pairingColumns}
         FROM pairings
         WHERE pairing_id = ?`,
      )
      .bind(pairingId)
      .first<Pairing>();
  }

  async markPairingExpired(pairingId: string): Promise<void> {
    await this.session
      .prepare(
        "UPDATE pairings SET status = 'expired' WHERE pairing_id = ? AND status = 'pending'",
      )
      .bind(pairingId)
      .run();
  }

  async findEntitlement(telegramId: number): Promise<Entitlement | null> {
    return this.session
      .prepare(
        `SELECT ${entitlementColumns}
         FROM entitlements
         WHERE telegram_id = ?`,
      )
      .bind(telegramId)
      .first<Entitlement>();
  }

  async listActiveEntitlements(timestamp: string): Promise<ActiveEntitlement[]> {
    const result = await this.session
      .prepare(
        `SELECT
           entitlement.telegram_id AS telegramId,
           entitlement.expires_at AS expiresAt,
           entitlement.max_devices AS maxDevices,
           entitlement.display_name AS displayName,
           entitlement.username,
           (
             SELECT COUNT(*)
             FROM devices
             WHERE devices.telegram_id = entitlement.telegram_id
               AND devices.revoked_at IS NULL
           ) AS activeDevices
         FROM entitlements AS entitlement
         WHERE entitlement.status = 'active'
           AND (entitlement.expires_at IS NULL OR entitlement.expires_at > ?)
         ORDER BY entitlement.telegram_id ASC`,
      )
      .bind(timestamp)
      .all<ActiveEntitlement>();
    return result.results;
  }

  async findDevice(deviceId: string): Promise<Device | null> {
    return this.session
      .prepare(
        `SELECT ${deviceColumns}
         FROM devices
         WHERE device_id = ?`,
      )
      .bind(deviceId)
      .first<Device>();
  }

  async findDeviceSession(deviceId: string): Promise<DeviceSession | null> {
    return this.session
      .prepare(
        `SELECT ${sessionColumns}
         FROM device_sessions
         WHERE device_id = ?`,
      )
      .bind(deviceId)
      .first<DeviceSession>();
  }

  async saveChallenge(challenge: ChallengeUpsert): Promise<void> {
    await this.session.batch([
      this.session
        .prepare("DELETE FROM challenges WHERE expires_at <= ?")
        .bind(challenge.createdAt),
      this.session
        .prepare(
          `INSERT INTO challenges (challenge_hash, device_id, created_at, expires_at)
           VALUES (?, ?, ?, ?)
           ON CONFLICT(device_id) DO UPDATE SET
             challenge_hash = excluded.challenge_hash,
             created_at = excluded.created_at,
             expires_at = excluded.expires_at`,
        )
        .bind(
          challenge.challengeHash,
          challenge.deviceId,
          challenge.createdAt,
          challenge.expiresAt,
        ),
    ]);
  }

  async findChallenge(
    challengeHash: string,
    deviceId: string,
  ): Promise<Challenge | null> {
    return this.session
      .prepare(
        `SELECT
           expires_at AS expiresAt
         FROM challenges
         WHERE challenge_hash = ? AND device_id = ?`,
      )
      .bind(challengeHash, deviceId)
      .first<Challenge>();
  }

  async consumeChallenge(
    challengeHash: string,
    deviceId: string,
  ): Promise<boolean> {
    const result = await this.session
      .prepare(
        "DELETE FROM challenges WHERE challenge_hash = ? AND device_id = ?",
      )
      .bind(challengeHash, deviceId)
      .run();
    return result.meta.changes === 1;
  }

  async markDeviceSeen(deviceId: string, timestamp: string): Promise<void> {
    await this.session
      .prepare("UPDATE devices SET last_seen_at = ? WHERE device_id = ?")
      .bind(timestamp, deviceId)
      .run();
  }

  async activatePairing(activation: PairingActivation): Promise<boolean> {
    // The device, rotating session, pairing transition, and entitlement
    // metadata must commit together.
    const results = await this.session.batch([
      this.session
        .prepare(
          `INSERT INTO devices (device_id, telegram_id, public_key, created_at, last_seen_at, revoked_at)
           SELECT ?, ?, ?, ?, ?, NULL
           WHERE EXISTS (
             SELECT 1 FROM pairings
             WHERE pairing_id = ? AND device_id = ? AND public_key = ?
               AND session_id = ? AND refresh_token_hash = ? AND fingerprint_hash = ?
               AND status = 'pending' AND expires_at > ?
           )
           AND EXISTS (
             SELECT 1 FROM entitlements
             WHERE telegram_id = ? AND status = 'active'
               AND (expires_at IS NULL OR expires_at > ?)
           )
           AND (
             EXISTS (
               SELECT 1 FROM devices
               WHERE device_id = ? AND telegram_id = ? AND revoked_at IS NULL
             )
             OR (
               SELECT COUNT(*) FROM devices
               WHERE telegram_id = ? AND revoked_at IS NULL
             ) < (
               SELECT max_devices FROM entitlements WHERE telegram_id = ?
             )
           )
           ON CONFLICT(device_id) DO UPDATE SET
             telegram_id = excluded.telegram_id,
             public_key = excluded.public_key,
             last_seen_at = excluded.last_seen_at,
             revoked_at = NULL
           WHERE devices.revoked_at IS NOT NULL
              OR (
                devices.telegram_id = excluded.telegram_id
                AND EXISTS (
                  SELECT 1 FROM device_sessions
                  WHERE device_id = devices.device_id AND fingerprint_hash = ?
                )
              )`,
        )
        .bind(
          activation.deviceId,
          activation.telegramId,
          activation.publicKey,
          activation.timestamp,
          activation.timestamp,
          activation.pairingId,
          activation.deviceId,
          activation.publicKey,
          activation.sessionId,
          activation.refreshTokenHash,
          activation.fingerprintHash,
          activation.timestamp,
          activation.telegramId,
          activation.timestamp,
          activation.deviceId,
          activation.telegramId,
          activation.telegramId,
          activation.telegramId,
          activation.fingerprintHash,
        ),
      this.session
        .prepare(
          `INSERT INTO device_sessions
             (device_id, session_id, generation, refresh_token_hash,
              previous_refresh_token_hash, last_rotation_id, fingerprint_hash, updated_at)
           SELECT ?, ?, 1, ?, NULL, NULL, ?, ?
           WHERE EXISTS (
             SELECT 1 FROM devices
             WHERE device_id = ? AND telegram_id = ? AND revoked_at IS NULL
           )
           AND EXISTS (
             SELECT 1 FROM pairings
             WHERE pairing_id = ? AND device_id = ? AND public_key = ?
               AND session_id = ? AND refresh_token_hash = ? AND fingerprint_hash = ?
               AND status = 'pending' AND expires_at > ?
           )
           ON CONFLICT(device_id) DO UPDATE SET
             session_id = excluded.session_id,
             generation = 1,
             refresh_token_hash = excluded.refresh_token_hash,
             previous_refresh_token_hash = NULL,
             last_rotation_id = NULL,
             fingerprint_hash = excluded.fingerprint_hash,
             updated_at = excluded.updated_at
           WHERE device_sessions.fingerprint_hash = excluded.fingerprint_hash`,
        )
        .bind(
          activation.deviceId,
          activation.sessionId,
          activation.refreshTokenHash,
          activation.fingerprintHash,
          activation.timestamp,
          activation.deviceId,
          activation.telegramId,
          activation.pairingId,
          activation.deviceId,
          activation.publicKey,
          activation.sessionId,
          activation.refreshTokenHash,
          activation.fingerprintHash,
          activation.timestamp,
        ),
      this.session
        .prepare(
          `UPDATE pairings SET telegram_id = ?, status = 'active'
           WHERE pairing_id = ? AND status = 'pending' AND expires_at > ?
             AND EXISTS (
               SELECT 1 FROM devices
               WHERE device_id = pairings.device_id AND telegram_id = ? AND revoked_at IS NULL
             )
             AND EXISTS (
               SELECT 1 FROM device_sessions
               WHERE device_id = pairings.device_id AND session_id = pairings.session_id
             )`,
        )
        .bind(
          activation.telegramId,
          activation.pairingId,
          activation.timestamp,
          activation.telegramId,
        ),
      this.session
        .prepare(
          `UPDATE entitlements SET display_name = ?, username = ?, updated_at = ?
           WHERE telegram_id = ? AND EXISTS (
             SELECT 1 FROM pairings
             WHERE pairing_id = ? AND telegram_id = ? AND status = 'active'
           )`,
        )
        .bind(
          activation.displayName,
          activation.username,
          activation.timestamp,
          activation.telegramId,
          activation.pairingId,
          activation.telegramId,
        ),
    ]);
    return results[2].meta.changes === 1;
  }

  async rotateSession(rotation: SessionRotation): Promise<"rotated" | "retry" | "superseded"> {
    const results = await this.session.batch([
      this.session
        .prepare(
          `UPDATE device_sessions
           SET previous_refresh_token_hash = refresh_token_hash,
               refresh_token_hash = ?,
               generation = generation + 1,
               last_rotation_id = ?,
               updated_at = ?
           WHERE device_id = ? AND session_id = ? AND generation = ?
             AND refresh_token_hash = ? AND fingerprint_hash = ?
             AND EXISTS (
               SELECT 1 FROM challenges
               WHERE challenge_hash = ? AND device_id = ? AND expires_at > ?
             )`,
        )
        .bind(
          rotation.nextRefreshTokenHash,
          rotation.rotationId,
          rotation.timestamp,
          rotation.deviceId,
          rotation.sessionId,
          rotation.generation,
          rotation.refreshTokenHash,
          rotation.fingerprintHash,
          rotation.challengeHash,
          rotation.deviceId,
          rotation.timestamp,
        ),
      this.session
        .prepare(
          "DELETE FROM challenges WHERE challenge_hash = ? AND device_id = ?",
        )
        .bind(rotation.challengeHash, rotation.deviceId),
      this.session
        .prepare("UPDATE devices SET last_seen_at = ? WHERE device_id = ?")
        .bind(rotation.timestamp, rotation.deviceId),
    ]);
    if (results[0].meta.changes === 1) return "rotated";

    const current = await this.findDeviceSession(rotation.deviceId);
    if (
      current?.sessionId === rotation.sessionId &&
      current.generation === rotation.generation + 1 &&
      current.refreshTokenHash === rotation.nextRefreshTokenHash &&
      current.previousRefreshTokenHash === rotation.refreshTokenHash &&
      current.lastRotationId === rotation.rotationId &&
      current.fingerprintHash === rotation.fingerprintHash
    )
      return "retry";
    return "superseded";
  }

  async insertDownloadGrants(grants: readonly DownloadGrant[]): Promise<void> {
    if (grants.length === 0) return;
    await this.session.batch(
      grants.map((grant) =>
        this.session
          .prepare(
            `INSERT INTO download_grants
               (jti, telegram_id, object_path, expires_at, consumed_at)
             VALUES (?, ?, ?, ?, NULL)`,
          )
          .bind(grant.jti, grant.telegramId, grant.objectPath, grant.expiresAt),
      ),
    );
  }

  async consumeDownloadGrant(
    jti: string,
    telegramId: number,
    objectPath: string,
    timestamp: string,
  ): Promise<boolean> {
    const result = await this.session
      .prepare(
        `UPDATE download_grants
         SET consumed_at = ?
         WHERE jti = ? AND telegram_id = ? AND object_path = ?
           AND consumed_at IS NULL AND expires_at > ?`,
      )
      .bind(timestamp, jti, telegramId, objectPath, timestamp)
      .run();
    return result.meta.changes === 1;
  }

  async upsertEntitlement(grant: EntitlementGrant): Promise<void> {
    await this.session
      .prepare(
        `INSERT INTO entitlements
           (telegram_id, status, expires_at, max_devices, display_name, username, created_at, updated_at)
         VALUES (?, 'active', ?, ?, '', '', ?, ?)
         ON CONFLICT(telegram_id) DO UPDATE SET
           status = 'active',
           expires_at = excluded.expires_at,
           max_devices = excluded.max_devices,
           updated_at = excluded.updated_at`,
      )
      .bind(
        grant.telegramId,
        grant.expiresAt,
        grant.maxDevices,
        grant.timestamp,
        grant.timestamp,
      )
      .run();
  }

  async revokeEntitlement(
    telegramId: number,
    timestamp: string,
  ): Promise<void> {
    await this.session.batch([
      this.session
        .prepare(
          "UPDATE entitlements SET status = 'revoked', updated_at = ? WHERE telegram_id = ?",
        )
        .bind(timestamp, telegramId),
      this.session
        .prepare(
          "UPDATE devices SET revoked_at = ? WHERE telegram_id = ? AND revoked_at IS NULL",
        )
        .bind(timestamp, telegramId),
      this.session
        .prepare(
          `DELETE FROM challenges
           WHERE device_id IN (SELECT device_id FROM devices WHERE telegram_id = ?)`,
        )
        .bind(telegramId),
      this.session
        .prepare(
          `DELETE FROM device_sessions
           WHERE device_id IN (SELECT device_id FROM devices WHERE telegram_id = ?)`,
        )
        .bind(telegramId),
    ]);
  }

  async listDevices(telegramId: number): Promise<DeviceSummary[]> {
    const result = await this.session
      .prepare(
        `SELECT
           device_id AS deviceId,
           last_seen_at AS lastSeenAt,
           revoked_at AS revokedAt
         FROM devices
         WHERE telegram_id = ?
         ORDER BY created_at ASC`,
      )
      .bind(telegramId)
      .all<DeviceSummary>();
    return result.results;
  }

  async revokeDevice(deviceId: string, timestamp: string): Promise<void> {
    await this.session.batch([
      this.session
        .prepare("UPDATE devices SET revoked_at = ? WHERE device_id = ?")
        .bind(timestamp, deviceId),
      this.session
        .prepare("DELETE FROM challenges WHERE device_id = ?")
        .bind(deviceId),
      this.session
        .prepare("DELETE FROM device_sessions WHERE device_id = ?")
        .bind(deviceId),
    ]);
  }

  async listTelegramCommandAdmins(): Promise<number[]> {
    const result = await this.session
      .prepare(
        `SELECT telegram_id AS telegramId
         FROM telegram_command_admins
         ORDER BY telegram_id ASC`,
      )
      .all<TelegramCommandAdmin>();
    return result.results.map(({ telegramId }) => telegramId);
  }

  async replaceTelegramCommandAdmins(
    telegramIds: readonly number[],
  ): Promise<void> {
    const statements = [
      this.session.prepare("DELETE FROM telegram_command_admins"),
      ...telegramIds.map((telegramId) =>
        this.session
          .prepare(
            "INSERT INTO telegram_command_admins (telegram_id) VALUES (?)",
          )
          .bind(telegramId),
      ),
    ];
    await this.session.batch(statements);
  }
}
