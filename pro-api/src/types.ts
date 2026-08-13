export interface Lease {
  schemaVersion: 1;
  deviceId: string;
  sessionId: string;
  generation: number;
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

export interface TelegramChat {
  id: number;
  type: "private" | "group" | "supergroup" | "channel";
}

export interface TelegramUpdate {
  message?: {
    chat: TelegramChat;
    from?: TelegramUser;
    text?: string;
  };
  callbackQuery?: {
    id: string;
    from: TelegramUser;
    data?: string;
    message?: {
      messageId: number;
      chat: TelegramChat;
    };
  };
}

export type ReleaseChannel = "stable" | "dev";

export interface Manifest {
  schemaVersion: 1;
  edition: "pro";
  channel: ReleaseChannel;
  version: string;
  commit: string;
  publishedAt: string;
  notes: string;
  artifacts: Array<{
    target: string;
    name: string;
    compression: "gzip";
    size: number;
    sha256: string;
    executableSize: number;
    executableSha256: string;
  }>;
}

type DownloadTicketFields = {
  channel: ReleaseChannel;
  version: string;
  target: string;
  path: string;
  expiresAt: number;
};

export interface DeviceDownloadTicket extends DownloadTicketFields {
  deviceId: string;
}

export interface BootstrapDownloadTicket extends DownloadTicketFields {
  purpose: "bootstrap";
  jti: string;
  telegramId: number;
}

export type DownloadTicket = DeviceDownloadTicket | BootstrapDownloadTicket;
