export type Licensee = {
  status: string
  deviceId: string
  telegramId: number
  displayName: string
  username?: string
  expiresAt?: string
  offlineUntil?: string
}

export type LicenseStatus = {
  authorized: boolean
  deviceId: string
}

export type LicensePairing = {
  id: string
  activationUrl: string
  status: 'pending' | 'active' | 'expired'
  expiresAt: string
}
