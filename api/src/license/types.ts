export interface Lease {
  schemaVersion: 1;
  deviceId: string;
  sessionId: string;
  generation: number;
  telegramId: number;
  status: 'active';
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
