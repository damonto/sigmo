/** A submission snapshot of the chosen modem's active SIM. */
export interface CommunicationTarget {
  modemId: string
  profileId: string
}

/** A backend-owned contact, independent of communication history. */
export interface Contact {
  id: string
  name: string
  notes: string
  numbers: ContactNumber[]
  hasAvatar: boolean
  avatarColor: string
  updatedAt: string
}
export interface ContactNumber {
  label: string
  number: string
  region: string
  key: string
}
/** Present is verified against inventory; active and capability flags govern sending. */
export interface SIMLine {
  profileId: string
  modemId: string
  label: string
  number: string
  region: string
  kind: string
  seId: string
  deletionState: string
  successorId: string
  messageCount: number
  callCount: number
  present: boolean
  active: boolean
  canMessage: boolean
  canCall: boolean
}
export interface ThreadMessage {
  id: number
  conversationId: string
  profileId: string
  historyProfileId: string
  modemId: string
  sender: string
  recipient: string
  text: string
  timestamp: string
  incoming: boolean
  read: boolean
  status: string
  lineLabel: string
  lineNumber: string
}
export interface MessageThread {
  id: string
  participant: string
  latestMessage: ThreadMessage
  unreadCount: number
  markedUnread: boolean
}
export interface CommunicationPreference {
  participant: string
  profileId: string
  updatedAt: string
}
export interface HistoryPage<T> {
  items: T[]
  nextCursor: string
}
export interface ThreadPage extends HistoryPage<MessageThread> {
  backendId: string
  revision: number
  unreadCount: number
}
export interface HistoryFilter {
  q?: string
  cursor?: string
  modemId?: string
  profileId?: string
  missed?: string
}

/** Authoritative badge returned after a read acknowledgement. */
export interface CommunicationBadge {
  backendId: string
  revision: number
  unreadCount: number
}
