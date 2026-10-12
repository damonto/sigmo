import { enableAutoUnmount, shallowMount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

import EsimProfileSection from '@/components/esim/EsimProfileSection.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import type { EsimProfile } from '@/types/esim'

enableAutoUnmount(afterEach)

vi.mock('@/apis/esim', () => ({ useEsimApi: () => ({}) }))
vi.mock('@/apis/reminder', () => ({ useReminderApi: () => ({}) }))

describe('eSIM number display with real translations', () => {
  it.each([
    { locale: 'en', placeholder: 'No Number' },
    { locale: 'zh', placeholder: '无号码' },
  ])(
    'uses each profile region and translates missing numbers in $locale',
    ({ locale, placeholder }) => {
      const numbers = [
        { number: '64210647889', regionCode: 'NZ', expected: '+64 210 647 889' },
        { number: '447386042885', regionCode: 'GB', expected: '+44 7386 042885' },
        { number: '+8613800000000', regionCode: 'CN', expected: '+86 138 0000 0000' },
        { number: ' +12025550123 ', regionCode: 'US', expected: '+1 (202) 555-0123' },
        { number: '+12025550123', regionCode: 'CN', expected: '+1 (202) 555-0123' },
        { number: '+12025550123', regionCode: undefined, expected: '+1 (202) 555-0123' },
        { number: undefined, regionCode: 'CN', expected: placeholder },
        { number: '', regionCode: 'US', expected: placeholder },
        { number: '  ', regionCode: undefined, expected: placeholder },
      ]
      const profiles: EsimProfile[] = numbers.map(({ number, regionCode }, index) => ({
        id: String(index),
        seId: 'default',
        seLabel: 'eUICC',
        name: `Line ${index}`,
        iccid: `iccid-${index}`,
        enabled: index === 0,
        serviceProviderName: 'Carrier',
        profileName: `Line ${index}`,
        profileStateName: index === 0 ? 'enabled' : 'disabled',
        profileClass: 'operational',
        profileOwner: { mcc: '310', mnc: '260' },
        number,
        regionCode: regionCode ?? '',
      }))
      const i18n = createI18n({
        legacy: false,
        locale,
        fallbackLocale: false,
        messages: { en, zh },
      })
      const wrapper = shallowMount(EsimProfileSection, {
        props: { modemId: 'modem-1', profiles },
        global: { plugins: [i18n] },
      })

      expect(i18n.global.te('modemDetail.esim.lineNoNumber')).toBe(true)
      for (const { expected } of numbers) expect(wrapper.text()).toContain(expected)
      expect(wrapper.text()).not.toContain('modemDetail.esim.lineNoNumber')
      expect(wrapper.text()).not.toContain('iccid-')
    },
  )
})
