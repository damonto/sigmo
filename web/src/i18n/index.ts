import { createI18n } from 'vue-i18n'

import { detectLocale } from './locale'
import en from './locales/en'
import zh from './locales/zh'

export type { AppLocale } from './locale'

const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: detectLocale(),
  fallbackLocale: 'en',
  messages: { en, zh },
})

export default i18n
