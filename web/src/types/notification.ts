export type NotificationResponse = {
  sequenceNumber: string
  iccid: string
  smdp: string
  operation: string
}

export type NotificationsResponse = NotificationResponse[]
