export type ReleaseChannel = 'stable' | 'dev';

export interface Manifest {
  schemaVersion: 1;

  channel: ReleaseChannel;
  version: string;
  commit: string;
  publishedAt: string;
  notes: string;
  artifacts: Array<{
    target: string;
    name: string;
    compression: 'gzip';
    size: number;
    sha256: string;
    executableSize: number;
    executableSha256: string;
  }>;
}

interface DownloadTicketFields {
  channel: ReleaseChannel;
  version: string;
  target: string;
  path: string;
  expiresAt: number;
}

export interface DeviceDownloadTicket extends DownloadTicketFields {
  deviceId: string;
}

export interface BootstrapDownloadTicket extends DownloadTicketFields {
  purpose: 'bootstrap';
  jti: string;
  telegramId: number;
}

export type DownloadTicket = DeviceDownloadTicket | BootstrapDownloadTicket;
