export type NotificationResponse = {
  seId: string
  seLabel: string
  seEid?: string
  sequenceNumber: string
  iccid: string
  smdp: string
  operation: string
}

export type NotificationGroupResponse = {
  id: string
  label: string
  aid?: string
  eid?: string
  notifications: NotificationResponse[]
}

export type NotificationsResponse = {
  ses: NotificationGroupResponse[]
}
