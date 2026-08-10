export interface Lease {
  schemaVersion: 1;
  deviceId: string;
  telegramId: number;
  status: "active";
  displayName: string;
  username?: string;
  issuedAt: string;
  refreshAfter: string;
  expiresAt: string;
  entitlementExpiresAt?: string;
}

export interface SignedLease {
  lease: Lease;
  signature: string;
}

export interface TelegramUser {
  id: number;
  first_name: string;
  last_name?: string;
  username?: string;
}

export interface TelegramUpdate {
  message?: {
    chat: {
      id: number;
      type: "private" | "group" | "supergroup" | "channel";
    };
    from?: TelegramUser;
    text?: string;
  };
}

export interface Manifest {
  schemaVersion: 1;
  edition: "pro";
  channel: "stable" | "dev";
  version: string;
  commit: string;
  publishedAt: string;
  notes: string;
  artifacts: Array<{
    target: string;
    name: string;
    size: number;
    sha256: string;
  }>;
}

export interface DownloadTicket {
  deviceId: string;
  channel: string;
  version: string;
  target: string;
  path: string;
  expiresAt: number;
}
