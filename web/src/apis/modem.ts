import { fetchJson } from '@/lib/fetch'

import type {
  ModemDetailResponse,
  ModemListResponse,
  ModemSettings,
  ModemSettingsResponse,
  WiFiCallingSettings,
  WiFiCallingEmergencyAddressWebsheetResponse,
  WiFiCallingSettingsResponse,
  WiFiCallingWebsheetResponse,
} from '@/types/modem'

/**
 * Modem API
 * Centralized API definitions
 */
export const useModemApi = () => {
  /**
   * Fetch all modems
   * GET /api/v1/modems
   */
  const getModems = () => {
    return fetchJson<ModemListResponse>('modems')
  }

  /**
   * Fetch single modem by ID
   * GET /api/v1/modems/:id
   */
  const getModem = (id: string) => {
    return fetchJson<ModemDetailResponse>(`modems/${id}`)
  }

  const unlockSim = (id: string, pin: string) => {
    return fetchJson<void>(`modems/${id}/sim-unlocks`, {
      method: 'POST',
      body: JSON.stringify({ pin }),
    })
  }

  /**
   * Switch active SIM slot by identifier
   * PUT /api/v1/modems/:id/sim-slots/:identifier
   */
  const switchSimSlot = (id: string, identifier: string) => {
    return fetchJson<void>(`modems/${id}/sim-slots/${identifier}`, {
      method: 'PUT',
    })
  }

  /**
   * Update MSISDN
   * PUT /api/v1/modems/:id/msisdn
   */
  const updateMsisdn = (id: string, number: string) => {
    return fetchJson<void>(`modems/${id}/msisdn`, {
      method: 'PUT',
      body: JSON.stringify({ number }),
    })
  }

  /**
   * Fetch modem settings
   * GET /api/v1/modems/:id/settings
   */
  const getSettings = (id: string) => {
    return fetchJson<ModemSettingsResponse>(`modems/${id}/settings`)
  }

  /**
   * Update modem settings
   * PUT /api/v1/modems/:id/settings
   */
  const updateSettings = (id: string, payload: ModemSettings) => {
    return fetchJson<void>(`modems/${id}/settings`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  /**
   * Fetch Wi-Fi Calling settings
   * GET /api/v1/modems/:id/wifi-calling-settings
   */
  const getWiFiCallingSettings = (id: string) => {
    return fetchJson<WiFiCallingSettingsResponse>(`modems/${id}/wifi-calling-settings`)
  }

  /**
   * Update Wi-Fi Calling settings
   * PUT /api/v1/modems/:id/wifi-calling-settings
   */
  const updateWiFiCallingSettings = (id: string, payload: WiFiCallingSettings) => {
    return fetchJson<void>(`modems/${id}/wifi-calling-settings`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  const startWiFiCallingWebsheet = (id: string) => {
    return fetchJson<WiFiCallingWebsheetResponse>(`modems/${id}/wifi-calling-websheets`, {
      method: 'POST',
    })
  }

  const startWiFiCallingEmergencyAddressWebsheet = (id: string) => {
    return fetchJson<WiFiCallingEmergencyAddressWebsheetResponse>(
      `modems/${id}/wifi-calling-emergency-address-websheets`,
      {
        method: 'POST',
      },
    )
  }

  return {
    getModems,
    getModem,
    unlockSim,
    switchSimSlot,
    updateMsisdn,
    getSettings,
    updateSettings,
    getWiFiCallingSettings,
    updateWiFiCallingSettings,
    startWiFiCallingWebsheet,
    startWiFiCallingEmergencyAddressWebsheet,
  }
}
