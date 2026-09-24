// Locale detection lives apart from the vue-i18n instance so that low-level
// modules, such as the fetch client, can use it without importing vue-i18n.

const supportedLocales = ['en', 'zh'] as const
export type AppLocale = (typeof supportedLocales)[number]

const pickLocale = (languages: readonly string[]): AppLocale => {
  for (const language of languages) {
    const normalized = language.toLowerCase()
    if (normalized.startsWith('en')) return 'en'
    if (normalized.startsWith('zh')) return 'zh'
  }
  return 'en'
}

/** Picks the UI language from the browser's preferred languages. */
export const detectLocale = (): AppLocale => {
  if (typeof navigator === 'undefined') {
    return 'en'
  }

  return pickLocale(navigator.languages ?? [navigator.language])
}
