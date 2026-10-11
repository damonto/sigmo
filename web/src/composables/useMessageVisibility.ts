import { onUnmounted, watch, type Ref } from 'vue'

/** Acknowledges the highest message actually visible inside the active timeline. */
export function useMessageVisibility(
  container: Readonly<Ref<HTMLElement | null>>,
  items: Readonly<Ref<readonly { key: string }[]>>,
  acknowledge: (id: number) => Promise<void>,
) {
  let observer: IntersectionObserver | undefined
  let highest = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  watch(
    [container, items],
    () => {
      observer?.disconnect()
      clearTimeout(timer)
      highest = 0
      const root = container.value
      if (!root || typeof IntersectionObserver === 'undefined') return
      observer = new IntersectionObserver(
        (entries) => {
          if (document.visibilityState !== 'visible') return
          for (const entry of entries) {
            if (!(entry.target instanceof HTMLElement) || !entry.isIntersecting) continue
            const id = Number(entry.target.dataset.messageId)
            if (Number.isSafeInteger(id)) highest = Math.max(highest, id)
          }
          clearTimeout(timer)
          if (highest > 0) timer = setTimeout(() => void acknowledge(highest), 200)
        },
        { root, threshold: 0.1 },
      )
      for (const element of root.querySelectorAll('[data-message-id]')) observer.observe(element)
    },
    { flush: 'post' },
  )
  onUnmounted(() => {
    observer?.disconnect()
    clearTimeout(timer)
  })
}
