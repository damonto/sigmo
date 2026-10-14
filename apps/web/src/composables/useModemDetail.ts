import { computed, onScopeDispose, ref, watch } from 'vue'

import { useEsimApi } from '@/apis/esim'
import { useSEApi } from '@/apis/se'
import { useModemResource } from '@/composables/useModemResource'
import { sessionRevision } from '@/lib/authSession'
import type { EsimProfile, EsimProfileApiResponse } from '@/types/esim'
import type { SEsResponse } from '@/types/se'

/** Owns inventory for one device identity; status updates reuse its current snapshot. */
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
  let seRequest: Promise<void> | undefined
  let profileRequest: Promise<void> | undefined
  let detailRequest:
    | { id: string; revision: number; refreshingOverview: boolean; promise: Promise<void> }
    | undefined
  let generation = 0
  let disposed = false

  const isCurrentEsimModem = (id: string) =>
    !disposed && modem.value?.id === id && modem.value.simKind === 'euicc'

  const resetEsimData = () => {
    generation++
    seRequest = undefined
    profileRequest = undefined
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
      number: profile.number?.trim() ?? '',
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

  const readSEs = async (id: string) => {
    const current = generation
    isSELoading.value = true

    try {
      const { data } = await seApi.getSEs(id)
      if (current !== generation || !isCurrentEsimModem(id)) return
      seInfo.value = data.value ?? null
    } catch (err) {
      if (current !== generation || !isCurrentEsimModem(id)) return
      console.error('[useModemDetail] Failed to fetch SE info:', err)
      seInfo.value = null
    } finally {
      if (current === generation) {
        isSELoading.value = false
      }
    }
  }

  const readEsimProfiles = async (id: string) => {
    const current = generation
    isEsimProfilesLoading.value = true
    try {
      const { data } = await esimApi.getEsims(id)
      if (current !== generation || !isCurrentEsimModem(id)) return
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
      if (current !== generation || !isCurrentEsimModem(id)) return
      console.error('[useModemDetail] Failed to fetch eSIM profiles:', err)
      esimProfiles.value = []
    } finally {
      if (current === generation) {
        isEsimProfilesLoading.value = false
      }
    }
  }

  const fetchSEs = (id: string): Promise<void> => {
    if (!isCurrentEsimModem(id)) return Promise.resolve()
    if (seRequest) return seRequest
    const request = readSEs(id).finally(() => {
      if (seRequest === request) seRequest = undefined
    })
    seRequest = request
    return request
  }
  const fetchEsimProfiles = (id: string): Promise<void> => {
    if (!isCurrentEsimModem(id)) return Promise.resolve()
    if (profileRequest) return profileRequest
    const request = readEsimProfiles(id).finally(() => {
      if (profileRequest === request) profileRequest = undefined
    })
    profileRequest = request
    return request
  }

  const fetchModemDetail = (id: string): Promise<void> => {
    if (disposed) return Promise.resolve()
    const revision = sessionRevision()
    if (detailRequest?.id === id && detailRequest.revision === revision)
      return detailRequest.promise

    // Publish ownership before changing modemId, which can synchronously rebind the resource.
    const request = {
      id,
      revision,
      refreshingOverview: true,
      promise: Promise.resolve()
        .then(async () => {
          if (detailRequest !== request) return
          await refreshModemResource()
          if (detailRequest !== request || revision !== sessionRevision()) return
          request.refreshingOverview = false
          await Promise.all([fetchSEs(id), fetchEsimProfiles(id)])
        })
        .finally(() => {
          if (detailRequest === request) detailRequest = undefined
        }),
    }
    detailRequest = request
    modemId.value = id
    return request.promise
  }

  watch(
    sessionRevision,
    () => {
      detailRequest = undefined
      resetEsimData()
    },
    { flush: 'sync' },
  )

  watch(
    [
      () => modem.value?.id,
      () => modem.value?.simKind,
      () => modem.value?.sim.identifier,
      () => modem.value?.sim.slot,
    ],
    () => {
      resetEsimData()
      const current = modem.value
      if (!current || detailRequest?.refreshingOverview) return
      void fetchSEs(current.id)
      void fetchEsimProfiles(current.id)
    },
    { flush: 'sync' },
  )
  onScopeDispose(() => {
    disposed = true
    detailRequest = undefined
    resetEsimData()
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
