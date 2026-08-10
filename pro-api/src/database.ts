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

type Pairing = {
  pollTokenHash: string;
  deviceId: string;
  publicKey: string;
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

type PairingInsert = {
  pairingId: string;
  pollTokenHash: string;
  deviceId: string;
  publicKey: string;
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
  telegramId: number;
  displayName: string;
  username: string;
  timestamp: string;
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
  telegram_id AS telegramId,
  status,
  expires_at AS expiresAt`;

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
    ]);
  }

  async insertPairing(
    pairing: PairingInsert,
    maxOutstanding: number,
  ): Promise<boolean> {
    const result = await this.session
      .prepare(
        `INSERT INTO pairings
           (pairing_id, poll_token_hash, device_id, public_key, telegram_id, status, created_at, expires_at)
         SELECT ?, ?, ?, ?, NULL, 'pending', ?, ?
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
    // The device limit, pairing transition, and licensee metadata must commit
    // together. D1 batch provides the transaction boundary for this flow.
    const results = await this.session.batch([
      this.session
        .prepare(
          `INSERT INTO devices (device_id, telegram_id, public_key, created_at, last_seen_at, revoked_at)
           SELECT ?, ?, ?, ?, ?, NULL
           WHERE EXISTS (
             SELECT 1 FROM pairings
             WHERE pairing_id = ? AND device_id = ? AND public_key = ?
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
           WHERE devices.telegram_id = excluded.telegram_id OR devices.revoked_at IS NOT NULL`,
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
          activation.timestamp,
          activation.telegramId,
          activation.timestamp,
          activation.deviceId,
          activation.telegramId,
          activation.telegramId,
          activation.telegramId,
        ),
      this.session
        .prepare(
          `UPDATE pairings SET telegram_id = ?, status = 'active'
           WHERE pairing_id = ? AND status = 'pending' AND expires_at > ?
             AND EXISTS (
               SELECT 1 FROM devices
               WHERE device_id = pairings.device_id AND telegram_id = ? AND revoked_at IS NULL
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
    return results[1].meta.changes === 1;
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
    await this.session
      .prepare("UPDATE devices SET revoked_at = ? WHERE device_id = ?")
      .bind(timestamp, deviceId)
      .run();
  }
}
