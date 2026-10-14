import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'

import { dialStringChars, formatDialInput } from '@/lib/phoneNumberInput'

/** Keeps keypad entry, formatting and long-press behavior consistent across call pages. */
export const useDialpadInput = (phoneCountry: Readonly<Ref<string>>) => {
  const digits = ref('')
  const plusLongPressTimer = ref<number | null>(null)
  const suppressNextZeroClick = ref(false)
  const keys = [
    { value: '1', letters: '' },
    { value: '2', letters: 'ABC' },
    { value: '3', letters: 'DEF' },
    { value: '4', letters: 'GHI' },
    { value: '5', letters: 'JKL' },
    { value: '6', letters: 'MNO' },
    { value: '7', letters: 'PQRS' },
    { value: '8', letters: 'TUV' },
    { value: '9', letters: 'WXYZ' },
    { value: '*', letters: '' },
    { value: '0', letters: '+' },
    { value: '#', letters: '' },
  ]

  const normalizedDigits = computed(() => dialStringChars(digits.value))
  const dialInputClass = computed(() => {
    const length = normalizedDigits.value.length
    if (length > 20) return 'text-lg'
    if (length > 15) return 'text-xl'
    if (length > 10) return 'text-2xl'
    return 'text-3xl'
  })

  const setDigits = (value: string) => {
    digits.value = formatDialInput(value, phoneCountry.value)
  }

  const appendDigit = (value: string) => {
    setDigits(`${digits.value}${value}`)
  }

  const appendPlus = () => {
    if (dialStringChars(digits.value)) return
    setDigits('+')
  }

  const clearPlusLongPress = () => {
    if (plusLongPressTimer.value === null) return
    window.clearTimeout(plusLongPressTimer.value)
    plusLongPressTimer.value = null
  }

  const startPlusLongPress = (value: string) => {
    if (value !== '0') return
    clearPlusLongPress()
    suppressNextZeroClick.value = false
    plusLongPressTimer.value = window.setTimeout(() => {
      appendPlus()
      suppressNextZeroClick.value = true
      plusLongPressTimer.value = null
    }, 450)
  }

  const appendKey = (value: string) => {
    if (value === '0' && suppressNextZeroClick.value) {
      suppressNextZeroClick.value = false
      return
    }
    appendDigit(value)
  }

  const backspace = () => {
    setDigits(dialStringChars(digits.value).slice(0, -1))
  }

  watch(phoneCountry, () => setDigits(digits.value))
  onBeforeUnmount(clearPlusLongPress)

  return {
    digits,
    normalizedDigits,
    keys,
    dialInputClass,
    setDigits,
    appendKey,
    backspace,
    startPlusLongPress,
    clearPlusLongPress,
  }
}
