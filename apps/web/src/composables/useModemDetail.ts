import { computed, ref, watch } from 'vue'

import { useEsimApi } from '@/apis/esim'
import { useSEApi } from '@/apis/se'
import { useModemResource } from '@/composables/useModemResource'
import type { EsimProfile, EsimProfileApiResponse } from '@/types/esim'
import type { SEsResponse } from '@/types/se'

export const useModemDetail = () => {
  const esimApi = useEsimApi()
  const seApi = useSEApi()

  const modemId = ref('')
  const {
    modem,
    isLoading,
    error: modemError,
    refresh: refreshModemResource,
  } = useModemResource(computed(() => modemId.value))
  const seInfo = ref<SEsResponse | null>(null)
  const esimProfiles = ref<EsimProfile[]>([])
  const isSELoading = ref(false)
  const isEsimProfilesLoading = ref(false)
  let seRequestID = 0
  let esimProfilesRequestID = 0

  const isCurrentEsimModem = (id: string) =>
    modem.value?.id === id && modem.value.simKind === 'euicc'

  const resetEsimData = () => {
    seRequestID++
    esimProfilesRequestID++
    seInfo.value = null
    esimProfiles.value = []
    isSELoading.value = false
    isEsimProfilesLoading.value = false
  }

  const mapEsimProfile = (profile: EsimProfileApiResponse): EsimProfile => {
    return {
      id: `${profile.seId}:${profile.iccid}`,
      seId: profile.seId,
      seLabel: profile.seLabel,
      seEid: profile.seEid,
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
      reminder: profile.reminder,
    }
  }

  const fetchSEs = async (id: string) => {
    const requestID = ++seRequestID
    isSELoading.value = true

    try {
      const { data } = await seApi.getSEs(id)
      if (requestID !== seRequestID || !isCurrentEsimModem(id)) return
      seInfo.value = data.value ?? null
    } catch (err) {
      if (requestID !== seRequestID || !isCurrentEsimModem(id)) return
      console.error('[useModemDetail] Failed to fetch SE info:', err)
      seInfo.value = null
    } finally {
      if (requestID === seRequestID) {
        isSELoading.value = false
      }
    }
  }

  const fetchEsimProfiles = async (id: string) => {
    const requestID = ++esimProfilesRequestID
    isEsimProfilesLoading.value = true
    try {
      const { data } = await esimApi.getEsims(id)
      if (requestID !== esimProfilesRequestID || !isCurrentEsimModem(id)) return
      if (data.value) {
        esimProfiles.value = data.value.ses.flatMap((group) =>
          group.profiles.map((profile) =>
            mapEsimProfile({
              ...profile,
              seId: profile.seId || group.id,
              seLabel: profile.seLabel || group.label,
              seEid: profile.seEid || group.eid,
            }),
          ),
        )
      } else {
        esimProfiles.value = []
      }
    } catch (err) {
      if (requestID !== esimProfilesRequestID || !isCurrentEsimModem(id)) return
      console.error('[useModemDetail] Failed to fetch eSIM profiles:', err)
      esimProfiles.value = []
    } finally {
      if (requestID === esimProfilesRequestID) {
        isEsimProfilesLoading.value = false
      }
    }
  }

  const fetchModemDetail = async (id: string) => {
    resetEsimData()
    modemId.value = id

    await refreshModemResource()
  }

  watch([modem, isLoading], ([current, loading]) => {
    if (loading) return
    if (!current || current.simKind !== 'euicc') {
      resetEsimData()
      return
    }
    void fetchSEs(current.id)
    void fetchEsimProfiles(current.id)
  })

  return {
    modem,
    seInfo,
    esimProfiles,
    isLoading,
    isSELoading,
    isEsimProfilesLoading,
    error: modemError,
    hasModem: computed(() => modem.value !== null),
    isSIMKindUnknown: computed(() => modem.value?.simKind === 'unknown'),
    isPhysicalModem: computed(() => modem.value?.simKind === 'physical'),
    isEsimModem: computed(() => modem.value?.simKind === 'euicc'),
    fetchModemDetail,
    fetchSEs,
    fetchEsimProfiles,
  }
}
