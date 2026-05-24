import { useFetch } from '@/lib/fetch'

import type {
  ModemDetailResponse,
  ModemListResponse,
  ModemSettings,
  ModemSettingsResponse,
  WiFiCallingSettings,
  WiFiCallingSettingsResponse,
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
    return useFetch<ModemListResponse>('modems').get().json()
  }

  /**
   * Fetch single modem by ID
   * GET /api/v1/modems/:id
   */
  const getModem = (id: string) => {
    return useFetch<ModemDetailResponse>(`modems/${id}`).get().json()
  }

  /**
   * Switch active SIM slot by identifier
   * PUT /api/v1/modems/:id/sim-slots/:identifier
   */
  const switchSimSlot = (id: string, identifier: string) => {
    return useFetch<void>(`modems/${id}/sim-slots/${identifier}`, {
      method: 'PUT',
    }).json()
  }

  /**
   * Update MSISDN
   * PUT /api/v1/modems/:id/msisdn
   */
  const updateMsisdn = (id: string, number: string) => {
    return useFetch<void>(`modems/${id}/msisdn`, {
      method: 'PUT',
      body: JSON.stringify({ number }),
    }).json()
  }

  /**
   * Fetch modem settings
   * GET /api/v1/modems/:id/settings
   */
  const getSettings = (id: string) => {
    return useFetch<ModemSettingsResponse>(`modems/${id}/settings`).get().json()
  }

  /**
   * Update modem settings
   * PUT /api/v1/modems/:id/settings
   */
  const updateSettings = (id: string, payload: ModemSettings) => {
    return useFetch<void>(`modems/${id}/settings`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }).json()
  }

  /**
   * Fetch Wi-Fi Calling settings
   * GET /api/v1/modems/:id/wifi-calling-settings
   */
  const getWiFiCallingSettings = (id: string) => {
    return useFetch<WiFiCallingSettingsResponse>(`modems/${id}/wifi-calling-settings`)
      .get()
      .json()
  }

  /**
   * Update Wi-Fi Calling settings
   * PUT /api/v1/modems/:id/wifi-calling-settings
   */
  const updateWiFiCallingSettings = (id: string, payload: WiFiCallingSettings) => {
    return useFetch<void>(`modems/${id}/wifi-calling-settings`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }).json()
  }

  return {
    getModems,
    getModem,
    switchSimSlot,
    updateMsisdn,
    getSettings,
    updateSettings,
    getWiFiCallingSettings,
    updateWiFiCallingSettings,
  }
}
