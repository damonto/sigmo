import type {
  PushDelivery,
  PushEnvironment,
  PushInstallation,
  PushSubscription,
} from '../push/types';

interface InstallationRegistration {
  id: string;
  managementHash: string;
  deviceToken: string;
  voipToken: string;
  environment: PushEnvironment;
  label: string;
}

const INSTALLATION_COLUMNS =
  'id, management_hash AS managementHash, device_token AS deviceToken, environment, label, token_revision AS tokenRevision, voip_token AS voipToken, voip_token_revision AS voipTokenRevision';
const SUBSCRIPTION_COLUMNS =
  'id, installation_id AS installationId, backend_id AS backendId, send_hash AS sendHash, enabled, sms, reminders, calls';

/** PushDatabase keeps community push permissions independent of Pro entitlements. */
export class PushDatabase {
  constructor(private readonly db: D1Database) {}

  installation(id: string): Promise<PushInstallation | null> {
    return this.db
      .prepare(
        `SELECT ${INSTALLATION_COLUMNS} FROM push_installations WHERE id = ?`,
      )
      .bind(id)
      .first<PushInstallation>();
  }

  async saveInstallation(
    value: InstallationRegistration,
    channels = { alert: true, voip: true },
  ): Promise<boolean> {
    const result = await this.db
      .prepare(
        `INSERT INTO push_installations
           (id, management_hash, device_token, environment, label, updated_at, voip_token)
         SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7
         WHERE NOT EXISTS (
           SELECT 1 FROM push_installations AS owner
           WHERE owner.id <> ?1 AND owner.environment = ?4
             AND ((?3 <> '' AND owner.device_token = ?3)
               OR (?7 <> '' AND owner.voip_token = ?7))
         )
         ON CONFLICT(id) DO UPDATE SET
           device_token = CASE
             WHEN ?8 THEN excluded.device_token
             WHEN push_installations.environment <> excluded.environment THEN ''
             ELSE push_installations.device_token
           END,
           voip_token = CASE
             WHEN ?9 THEN excluded.voip_token
             WHEN push_installations.environment <> excluded.environment THEN ''
             ELSE push_installations.voip_token
           END,
           voip_token_revision = push_installations.voip_token_revision + CASE
             WHEN (?9 AND push_installations.voip_token <> excluded.voip_token)
               OR push_installations.environment <> excluded.environment THEN 1
             ELSE 0
           END,
           token_revision = push_installations.token_revision + CASE
             WHEN (?8 AND push_installations.device_token <> excluded.device_token)
               OR push_installations.environment <> excluded.environment THEN 1
             ELSE 0
           END,
           environment = excluded.environment,
           label = excluded.label,
           updated_at = excluded.updated_at
         WHERE push_installations.management_hash = excluded.management_hash`,
      )
      .bind(
        value.id,
        value.managementHash,
        value.deviceToken,
        value.environment,
        value.label,
        Date.now(),
        value.voipToken,
        Number(channels.alert),
        Number(channels.voip),
      )
      .run();
    return result.meta.changes === 1;
  }

  async deleteInstallation(id: string): Promise<void> {
    await this.db
      .prepare('DELETE FROM push_installations WHERE id = ?')
      .bind(id)
      .run();
  }

  subscription(id: string): Promise<PushSubscription | null> {
    return this.db
      .prepare(
        `SELECT ${SUBSCRIPTION_COLUMNS} FROM push_subscriptions WHERE id = ?`,
      )
      .bind(id)
      .first<PushSubscription>();
  }

  async saveSubscription(value: PushSubscription): Promise<void> {
    await this.db.batch([
      this.db
        .prepare(
          'DELETE FROM push_subscriptions WHERE installation_id = ? AND backend_id = ? AND id <> ?',
        )
        .bind(value.installationId, value.backendId, value.id),
      this.db
        .prepare(
          `INSERT INTO push_subscriptions
             (id, installation_id, backend_id, send_hash, enabled, sms, reminders, calls)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT(id) DO UPDATE SET
             send_hash = excluded.send_hash,
             enabled = excluded.enabled,
             sms = excluded.sms,
             reminders = excluded.reminders,
             calls = excluded.calls,
             unread_count = CASE
               WHEN excluded.enabled = 0 OR excluded.sms = 0 THEN 0
               ELSE push_subscriptions.unread_count
             END
           WHERE push_subscriptions.installation_id = excluded.installation_id
             AND push_subscriptions.backend_id = excluded.backend_id`,
        )
        .bind(
          value.id,
          value.installationId,
          value.backendId,
          value.sendHash,
          value.enabled,
          value.sms,
          value.reminders,
          value.calls,
        ),
    ]);
  }

  async deleteSubscription(id: string): Promise<void> {
    await this.db
      .prepare('DELETE FROM push_subscriptions WHERE id = ?')
      .bind(id)
      .run();
  }

  async updateUnread(
    subscription: PushSubscription,
    delivery: PushDelivery,
  ): Promise<{ count: number; revision: number }> {
    await this.db.batch([
      this.db
        .prepare(
          `UPDATE push_subscriptions SET
             revision = ?,
             unread_count = CASE WHEN enabled = 1 AND sms = 1 THEN ? ELSE 0 END
           WHERE id = ? AND revision < ?`,
        )
        .bind(
          delivery.revision,
          delivery.unreadCount,
          subscription.id,
          delivery.revision,
        ),
      this.db
        .prepare(
          'UPDATE push_installations SET badge_revision = badge_revision + 1 WHERE id = ?',
        )
        .bind(subscription.installationId),
    ]);
    return this.badge(subscription.installationId);
  }

  async badge(installationId: string): Promise<{
    count: number;
    revision: number;
    backends: Array<{
      backendId: string;
      revision: number;
      unreadCount: number;
    }>;
  }> {
    const value = (await this.db
      .prepare(
        `SELECT badge_revision AS revision,
           COALESCE((
             SELECT SUM(unread_count) FROM push_subscriptions
             WHERE installation_id = ? AND enabled = 1 AND sms = 1
           ), 0) AS count
         FROM push_installations WHERE id = ?`,
      )
      .bind(installationId, installationId)
      .first<{ count: number; revision: number }>()) ?? {
      count: 0,
      revision: 0,
    };
    const rows = await this.db
      .prepare(
        'SELECT backend_id AS backendId, revision, CASE WHEN enabled = 1 AND sms = 1 THEN unread_count ELSE 0 END AS unreadCount FROM push_subscriptions WHERE installation_id = ?',
      )
      .bind(installationId)
      .all<{ backendId: string; revision: number; unreadCount: number }>();
    return { ...value, backends: rows.results };
  }

  // Zero permits this request; otherwise return the actual fixed-window wait.
  async rateLimit(
    key: string,
    limit: number,
    seconds: number,
  ): Promise<number> {
    const now = Date.now();
    const window = Math.floor(now / (seconds * 1000));
    const row = await this.db
      .prepare(
        `INSERT INTO push_rate_limits (id, window, count, expires_at)
         VALUES (?, ?, 1, ?)
         ON CONFLICT(id) DO UPDATE SET
           window = excluded.window,
           expires_at = excluded.expires_at,
           count = CASE
             WHEN push_rate_limits.window = excluded.window THEN push_rate_limits.count + 1
             ELSE 1
           END
         RETURNING count`,
      )
      .bind(key, window, now + seconds * 2000)
      .first<{ count: number }>();
    if (!row) {
      throw new Error('rate limit update returned no row');
    }
    return row.count <= limit
      ? 0
      : Math.max(1, Math.ceil(((window + 1) * seconds * 1000 - now) / 1000));
  }

  async claim(
    delivery: PushDelivery,
    lease: string,
  ): Promise<'claimed' | 'sent' | 'busy'> {
    const now = Date.now();
    const row = await this.db
      .prepare(
        `INSERT INTO push_deliveries
           (subscription_id, event_id, status, lease_token, lease_until, expires_at)
         VALUES (?, ?, 'pending', ?, ?, ?)
         ON CONFLICT(subscription_id, event_id) DO UPDATE SET
           lease_token = excluded.lease_token,
           lease_until = excluded.lease_until
         WHERE push_deliveries.status = 'pending' AND push_deliveries.lease_until <= ?
         RETURNING lease_token`,
      )
      .bind(
        delivery.subscriptionId,
        delivery.eventId,
        lease,
        now + 30_000,
        delivery.expiresAt * 1000 + 86_400_000,
        now,
      )
      .first<{ lease_token: string }>();
    if (row?.lease_token === lease) {
      return 'claimed';
    }
    const existing = await this.db
      .prepare(
        'SELECT status FROM push_deliveries WHERE subscription_id = ? AND event_id = ?',
      )
      .bind(delivery.subscriptionId, delivery.eventId)
      .first<{ status: string }>();
    return existing?.status === 'sent' ? 'sent' : 'busy';
  }

  async finish(
    delivery: PushDelivery,
    lease: string,
    sent: boolean,
  ): Promise<void> {
    await this.db
      .prepare(
        'UPDATE push_deliveries SET status = ?, lease_until = 0 WHERE subscription_id = ? AND event_id = ? AND lease_token = ?',
      )
      .bind(
        sent ? 'sent' : 'pending',
        delivery.subscriptionId,
        delivery.eventId,
        lease,
      )
      .run();
  }

  async invalidateToken(
    installation: PushInstallation,
    voip = false,
  ): Promise<boolean> {
    // A response to an older in-flight request must not revoke a newly registered token.
    const result = await this.db
      .prepare(
        voip
          ? `UPDATE push_installations SET voip_token = '' WHERE id = ? AND voip_token_revision = ?`
          : `UPDATE push_installations SET device_token = '' WHERE id = ? AND token_revision = ?`,
      )
      .bind(
        installation.id,
        voip ? installation.voipTokenRevision : installation.tokenRevision,
      )
      .run();
    return result.meta.changes > 0;
  }

  async clean(): Promise<void> {
    await this.db.batch([
      this.db
        .prepare('DELETE FROM push_deliveries WHERE expires_at <= ?')
        .bind(Date.now()),
      this.db
        .prepare('DELETE FROM push_rate_limits WHERE expires_at <= ?')
        .bind(Date.now()),
    ]);
  }
}
