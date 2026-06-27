import { computed, ref } from 'vue'

import { useEsimApi } from '@/apis/esim'
import { useEuiccApi } from '@/apis/euicc'
import { useModemResource } from '@/composables/useModemResource'
import type { EsimProfile, EsimProfileApiResponse } from '@/types/esim'
import type { EuiccApiResponse } from '@/types/euicc'

export const useModemDetail = () => {
  const esimApi = useEsimApi()
  const euiccApi = useEuiccApi()

  const modemId = ref('')
  const {
    modem,
    isLoading,
    error: modemError,
    refresh: refreshModemResource,
  } = useModemResource(computed(() => modemId.value))
  const euicc = ref<EuiccApiResponse | null>(null)
  const esimProfiles = ref<EsimProfile[]>([])
  const isEuiccLoading = ref(false)
  const isEsimProfilesLoading = ref(false)

  const mapEsimProfile = (profile: EsimProfileApiResponse): EsimProfile => {
    return {
      id: profile.iccid,
      name: profile.name,
      iccid: profile.iccid,
      isdPAID: profile.isdPAID,
      enabled: profile.profileState === 1,
      serviceProviderName: profile.serviceProviderName,
      profileName: profile.profileName,
      profileNickname: profile.profileNickname,
      profileStateName: profile.profileStateName,
      profileClass: profile.profileClass,
      profileOwner: profile.profileOwner,
      regionCode: profile.regionCode ?? '',
      logoUrl: profile.icon.length > 0 ? profile.icon : undefined,
    }
  }

  const fetchEuicc = async (id: string) => {
    isEuiccLoading.value = true

    try {
      const { data } = await euiccApi.getEuicc(id)

      if (data.value) {
        euicc.value = data.value
      }
    } catch (err) {
      console.error('[useModemDetail] Failed to fetch eUICC info:', err)
      euicc.value = null
    } finally {
      isEuiccLoading.value = false
    }
  }

  const fetchEsimProfiles = async (id: string) => {
    isEsimProfilesLoading.value = true
    try {
      const { data } = await esimApi.getEsims(id)
      if (data.value) {
        esimProfiles.value = data.value.map(mapEsimProfile)
      } else {
        esimProfiles.value = []
      }
    } catch (err) {
      console.error('[useModemDetail] Failed to fetch eSIM profiles:', err)
      esimProfiles.value = []
    } finally {
      isEsimProfilesLoading.value = false
    }
  }

  const fetchModemDetail = async (id: string) => {
    modemId.value = id
    euicc.value = null
    esimProfiles.value = []

    await refreshModemResource()
    if (modem.value?.supportsEsim) {
      void fetchEuicc(id)
      void fetchEsimProfiles(id)
    }
  }

  return {
    modem,
    euicc,
    esimProfiles,
    isLoading,
    isEuiccLoading,
    isEsimProfilesLoading,
    error: modemError,
    hasModem: computed(() => modem.value !== null),
    isPhysicalModem: computed(() => Boolean(modem.value && !modem.value.supportsEsim)),
    isEsimModem: computed(() => Boolean(modem.value && modem.value.supportsEsim)),
    fetchModemDetail,
    fetchEuicc,
    fetchEsimProfiles,
  }
}
