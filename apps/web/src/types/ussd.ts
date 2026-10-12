export type UssdAction = 'initialize' | 'reply'

export type UssdReply = {
  reply: string
}

export type UssdExecuteResponse = UssdReply
