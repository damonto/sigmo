export type MessageResponse = {
  id: number
  sender: string
  recipient: string
  text: string
  timestamp: string
  status: string
  incoming: boolean
}

export type MessagesResponse = MessageResponse[]
