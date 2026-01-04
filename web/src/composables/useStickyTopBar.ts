import { ref, watch, type Ref } from 'vue'

type StickyTopBarOptions = {
  offset?: number
}

export const useStickyTopBar = (
  sentinelRef: Ref<HTMLElement | null>,
  options: StickyTopBarOptions = {},
) => {
  const isStickyVisible = ref(false)
  const offset = options.offset ?? 0

  watch(
    () => sentinelRef.value,
    (element, _, onCleanup) => {
      if (!element || typeof window === 'undefined' || !('IntersectionObserver' in window)) {
        return
      }

      const rootMargin = offset > 0 ? `-${offset}px 0px 0px 0px` : '0px'
      const observer = new IntersectionObserver(
        (entries) => {
          const entry = entries[0]
          if (!entry) return
          isStickyVisible.value = !entry.isIntersecting
        },
        {
          root: null,
          threshold: 0,
          rootMargin,
        },
      )

      observer.observe(element)
      onCleanup(() => observer.disconnect())
    },
    { immediate: true },
  )

  return { isStickyVisible }
}
