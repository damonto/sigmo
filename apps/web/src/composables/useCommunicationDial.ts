import { inject, type InjectionKey } from 'vue'
import type { CommunicationTarget } from '@/types/communication'

/** Pins outgoing communication to the explicitly selected backend SIM. */
export type CommunicationDial = (number: string, line: CommunicationTarget) => Promise<boolean>
export const communicationDialKey: InjectionKey<CommunicationDial> = Symbol('communication-dial')
export const useCommunicationDial = () => inject(communicationDialKey)
