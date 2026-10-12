import { computed, ref, watch } from 'vue'

import { carrierLogoURL } from '@/lib/carrierLogo'

/** Reset image failures when the SIM identity or its embedded icon changes. */
export const useCarrierLogo = (icon: () => string | undefined, plmn: () => string | undefined) => {
  const candidates = computed(() => [...new Set([icon(), carrierLogoURL(plmn())].filter(Boolean))])
  const index = ref(0)
  const loaded = ref(false)
  const source = computed(() => candidates.value[index.value] ?? '')

  watch(
    candidates,
    () => {
      index.value = 0
    },
    { flush: 'sync' },
  )
  // Metadata can change while the selected URL stays the same. That image
  // will not emit another load event, so retain its loaded state.
  watch(
    source,
    () => {
      loaded.value = false
    },
    { flush: 'sync' },
  )

  const isCurrentImage = (event: Event) =>
    event.currentTarget instanceof HTMLImageElement &&
    event.currentTarget.getAttribute('src') === source.value

  const onLoad = (event: Event) => {
    if (isCurrentImage(event)) loaded.value = true
  }
  const onError = (event: Event) => {
    if (!isCurrentImage(event)) return
    loaded.value = false
    index.value++
  }

  return { source, loaded, onLoad, onError }
}
