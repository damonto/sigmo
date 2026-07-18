export type WebPushSubscriptionResponse = {
  id: string
  endpoint: string
  label: string
  userAgent: string
  platform: string
  createdAt: string
  updatedAt: string
}

export type WebPushOverviewResponse = {
  enabled: boolean
  publicKey: string
  subscriptions: WebPushSubscriptionResponse[]
}

export type WebPushRegisterPayload = {
  endpoint: string
  keys: {
    p256dh: string
    auth: string
  }
  label: string
  platform: string
}

export type WebPushPayload = {
  type: 'sms' | 'call' | 'reminder'
  id: string
  modemId: string
  modem: string
  from?: string
  text?: string
  profileType?: 'psim' | 'esim'
  profileId?: string
  profileName?: string
  url: string
  tag: string
}
