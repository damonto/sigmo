import { ref } from 'vue'

const canUseDom = typeof window !== 'undefined' && typeof document !== 'undefined'

const isDark = ref(false)

let isInitialized = false
let stopListener: (() => void) | null = null

const applyThemeClass = (value: boolean) => {
  if (!canUseDom) return
  document.documentElement.classList.toggle('dark', value)
}

const initThemeSync = () => {
  if (!canUseDom || isInitialized || !window.matchMedia) return
  isInitialized = true

  const media = window.matchMedia('(prefers-color-scheme: dark)')
  const update = (value: boolean) => {
    isDark.value = value
    applyThemeClass(value)
  }

  update(media.matches)

  const handleChange = (event: MediaQueryListEvent) => {
    update(event.matches)
  }

  media.addEventListener('change', handleChange)
  stopListener = () => media.removeEventListener('change', handleChange)
}

export const useTheme = () => {
  initThemeSync()
  return {
    isDark,
    stop: () => stopListener?.(),
  }
}
