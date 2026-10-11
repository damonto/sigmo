export type PushEnvironment = 'development' | 'production';

export interface PushInstallation {
  id: string;
  managementHash: string;
  deviceToken: string;
  environment: PushEnvironment;
  label: string;
  tokenRevision: number;
  voipToken: string;
  voipTokenRevision: number;
}

export interface PushSubscription {
  id: string;
  installationId: string;
  backendId: string;
  sendHash: string;
  enabled: number;
  sms: number;
  reminders: number;
  calls: number;
}

export interface PushEnvelope {
  version: 1;
  subscriptionId: string;
  eventId: string;
  nonce: string;
  ciphertext: string;
}

interface DeliveryFields {
  subscriptionId: string;
  eventId: string;
  revision: number;
  unreadCount: number;
  expiresAt: number;
}

interface EncryptedDelivery extends DeliveryFields {
  threadId: string;
  envelope: PushEnvelope;
}

export interface PushAlertDelivery extends EncryptedDelivery {
  kind: 'sms' | 'reminder';
}

interface PushCallDelivery extends EncryptedDelivery {
  kind: 'call';
  callUUID: string;
}

interface PushBadgeDelivery extends DeliveryFields {
  kind: 'badge';
}

export type PushDelivery =
  PushAlertDelivery | PushCallDelivery | PushBadgeDelivery;
