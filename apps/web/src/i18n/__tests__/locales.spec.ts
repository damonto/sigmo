import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const leafPaths = (value: unknown, prefix = ''): string[] => {
  if (typeof value === 'string') return [prefix]
  if (!value || typeof value !== 'object') return []

  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return leafPaths(child, path)
  })
}

describe('locales', () => {
  it('keeps English and Chinese keys in sync', () => {
    expect(leafPaths(zh).sort()).toEqual(leafPaths(en).sort())
  })

  it('compiles every localized message', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      fallbackLocale: 'en',
      messages: { en, zh },
    })

    const tests = [
      { locale: 'en', messages: en },
      { locale: 'zh', messages: zh },
    ] as const

    for (const tt of tests) {
      i18n.global.locale.value = tt.locale

      for (const path of leafPaths(tt.messages)) {
        expect(() => i18n.global.t(path)).not.toThrow()
      }
    }
  })

  it('renders email placeholders with literal at signs', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      fallbackLocale: 'en',
      messages: { en, zh },
    })

    expect(i18n.global.t('settings.schema.channels.email.smtpUsername.placeholder')).toBe(
      'user@example.com',
    )
    expect(i18n.global.t('settings.schema.channels.email.from.placeholder')).toBe(
      'Sigmo <sigmo@example.com>',
    )
  })

  it('uses the full Authentication label in English settings', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      fallbackLocale: 'en',
      messages: { en, zh },
    })
    const tests = [
      { path: 'settings.authTitle', want: 'Authentication' },
      { path: 'settings.schema.auth.authProviders.label', want: 'Authentication providers' },
    ] as const

    for (const tt of tests) {
      expect(i18n.global.t(tt.path)).toBe(tt.want)
    }
  })
})
