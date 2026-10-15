/** PIN1 state of the active SIM. Null values are unknown, never zero or disabled. */
export interface SimPinStatus {
  supported: boolean
  enabled: boolean | null
  lock: 'none' | 'pin' | 'puk' | 'permanently_blocked' | 'other' | 'unknown'
  pinRetries: number | null
  pukRetries: number | null
  simToken: string
}

/** Each command consumes at most one credential attempt. */
export type SimPinCommand =
  | { kind: 'protection'; enabled: boolean; pin: string; simToken: string }
  | { kind: 'change'; oldPin: string; newPin: string; simToken: string }
  | { kind: 'unlock'; pin: string; simToken: string }
  | { kind: 'unblock'; puk: string; newPin: string; simToken: string }
