import { computed, ref, watch, type Ref } from 'vue'

import type { EsimTransferRequest, EsimTransferSelection } from '@/types/esimPeer'

/** Keeps the source owner's selection bound to the currently displayed request. */
export const useEsimTransferRequestSelection = (
  request: Readonly<Ref<EsimTransferRequest | undefined>>,
) => {
  const sourceKey = ref('')
  const profileID = ref('')
  const sourceImei = ref('')
  const source = computed(() =>
    request.value?.sources?.find(
      (entry) => `${entry.source.type}:${entry.source.id}` === sourceKey.value,
    ),
  )
  const profiles = computed(
    () =>
      source.value?.profiles.filter((profile) => profile.supported && profile.type === 'esim') ??
      [],
  )
  const profile = computed(() => profiles.value.find((item) => item.id === profileID.value))
  const requiresSourceImei = computed(() => source.value?.source.requiresSourceImei ?? false)
  const selection = computed<EsimTransferSelection | undefined>(() => {
    const device = source.value?.source
    const selected = profile.value
    if (
      request.value?.phase !== 'authorization' ||
      !device ||
      !selected ||
      (requiresSourceImei.value && !/^\d{15}$/.test(sourceImei.value))
    )
      return undefined
    return {
      sourceType: device.type,
      sourceId: device.id,
      profileId: selected.id,
      sourceImei: requiresSourceImei.value ? sourceImei.value : undefined,
    }
  })
  watch(
    sourceKey,
    () => {
      profileID.value = ''
      sourceImei.value = ''
    },
    { flush: 'sync' },
  )
  watch(
    [() => request.value?.id, () => request.value?.revision],
    () => {
      sourceKey.value = ''
      profileID.value = ''
      sourceImei.value = ''
    },
    { flush: 'sync' },
  )
  return {
    sourceKey,
    profileID,
    sourceImei,
    source,
    profiles,
    profile,
    selection,
    requiresSourceImei,
  }
}
