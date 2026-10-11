export type MessageResponse = {
  id: number
  conversationId?: string
  profileId?: string
  sender: string
  recipient: string
  text: string
  timestamp: string
  status: string
  incoming: boolean
  wifiCalling: boolean
}

export type MessagesResponse = MessageResponse[]

export type SendMessageResponse = {
  to: string
  conversationId?: string
}

/** Backend-owned reading state; revision orders updates across clients. */
export interface MessageState {
  backendId: string
  revision: number
  unreadCount: number
}

/** A correspondent on one SIM, including inactive line history. */
export interface Conversation {
  id: string
  profileId: string
  participant: string
  modemId: string
  lineLabel: string
  lineNumber: string
  readThroughMessageId: number
  lastMessageId: number
  markedUnread: boolean
  unreadCount: number
  latestMessage: MessageResponse
  canSend: boolean
}

/** A filtered list with an unfiltered backend badge. */
export interface ConversationsResponse extends MessageState {
  items: Conversation[]
}

/** Acknowledges only the message IDs already displayed by this client. */
export interface ConversationRead {
  readThroughMessageId?: number
  markedUnread?: boolean
}
