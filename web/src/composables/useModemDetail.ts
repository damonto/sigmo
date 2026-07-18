import { computed, ref } from 'vue'

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
    isSELoading.value = true

    try {
      const { data } = await seApi.getSEs(id)

      if (data.value) {
        seInfo.value = data.value
      }
    } catch (err) {
      console.error('[useModemDetail] Failed to fetch SE info:', err)
      seInfo.value = null
    } finally {
      isSELoading.value = false
    }
  }

  const fetchEsimProfiles = async (id: string) => {
    isEsimProfilesLoading.value = true
    try {
      const { data } = await esimApi.getEsims(id)
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
      console.error('[useModemDetail] Failed to fetch eSIM profiles:', err)
      esimProfiles.value = []
    } finally {
      isEsimProfilesLoading.value = false
    }
  }

  const fetchModemDetail = async (id: string) => {
    modemId.value = id
    seInfo.value = null
    esimProfiles.value = []

    await refreshModemResource()
    if (modem.value?.supportsEsim) {
      void fetchSEs(id)
      void fetchEsimProfiles(id)
    }
  }

  return {
    modem,
    seInfo,
    esimProfiles,
    isLoading,
    isSELoading,
    isEsimProfilesLoading,
    error: modemError,
    hasModem: computed(() => modem.value !== null),
    isPhysicalModem: computed(() => Boolean(modem.value && !modem.value.supportsEsim)),
    isEsimModem: computed(() => Boolean(modem.value && modem.value.supportsEsim)),
    fetchModemDetail,
    fetchSEs,
    fetchEsimProfiles,
  }
}
