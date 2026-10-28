import type { EsimProfileOwner } from '@/types/esim'

/** Keep MNCs as strings: 310070 and 31070 are different network identities. */
export const profilePLMN = (owner?: EsimProfileOwner): string => {
  if (!owner || !/^[0-9]{3}$/.test(owner.mcc) || !/^[0-9]{2,3}$/.test(owner.mnc)) return ''
  return owner.mcc + owner.mnc
}

/** Public logo requests contain only the home network's PLMN, never credentials. */
export const carrierLogoURL = (plmn?: string): string => {
  if (!plmn || !/^[0-9]{5,6}$/.test(plmn)) return ''
  return `https://sigmo.952007.xyz/logo/${plmn}`
}
