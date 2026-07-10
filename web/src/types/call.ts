export type CallRoute = 'auto' | 'wifi_calling' | 'volte' | 'modem'
export type StoredCallRoute = Exclude<CallRoute, 'auto'>
export type CallDirection = 'incoming' | 'outgoing'
export type CallState =
  | 'dialing'
  | 'ringing'
  | 'answering'
  | 'early_media'
  | 'active'
  | 'confirmed'
  | 'ending'
  | 'ended'
  | 'failed'
export type CallHoldState = 'none' | 'local' | 'remote' | 'local_remote'

export type CallRecord = {
  callID: string
  route: StoredCallRoute
  direction: CallDirection
  number: string
  state: CallState
  hold: CallHoldState
  reason: string
  startedAt: string
  answeredAt: string
  endedAt: string
  updatedAt: string
}

export type DialCallRequest = {
  to: string
  route: CallRoute
}

export type UpdateCallRequest = {
  state?: 'active' | 'ended'
  reason?: 'busy' | ''
  hold?: 'local' | 'none'
}

export type SendDTMFRequest = {
  digits: string
}

export type CallEventMessage = {
  type: 'call'
  call: CallRecord
}

export type WebRTCSessionDescriptionPayload = {
  type: 'offer' | 'answer'
  sdp: string
}

export type WebRTCICEServersPayload = {
  iceServers: RTCIceServer[]
}

export type WebRTCSignalMessage =
  | {
      type: 'offer'
      offer: WebRTCSessionDescriptionPayload
    }
  | {
      type: 'answer'
      answer: WebRTCSessionDescriptionPayload
    }
  | {
      type: 'candidate'
      candidate: RTCIceCandidateInit
    }
  | {
      type: 'error'
      message: string
    }
