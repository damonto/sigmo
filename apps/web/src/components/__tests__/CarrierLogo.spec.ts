import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import CarrierLogo from '@/components/CarrierLogo.vue'
import { carrierLogoURL, profilePLMN } from '@/lib/carrierLogo'

describe('CarrierLogo', () => {
  it('uses the embedded logo first, then the PNG, then the local flag', async () => {
    const wrapper = mount(CarrierLogo, {
      props: {
        name: 'Carrier',
        icon: 'data:image/png;base64,broken',
        plmn: '310070',
        regionCode: 'US',
      },
    })
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,broken')
    expect(wrapper.find('.fi-us').exists()).toBe(true)
    await wrapper.get('img').trigger('error')
    expect(wrapper.get('img').attributes('src')).toBe(carrierLogoURL('310070'))
    await wrapper.get('img').trigger('load')
    expect(wrapper.get('img').isVisible()).toBe(true)
    expect(wrapper.find('.fi-us').exists()).toBe(false)
    await wrapper.get('img').trigger('error')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('.fi-us').exists()).toBe(true)
  })

  it('resets failed or loaded images when switching profiles', async () => {
    const wrapper = mount(CarrierLogo, {
      props: { name: 'First', plmn: '310070', regionCode: 'US' },
    })
    await wrapper.get('img').trigger('error')
    await wrapper.setProps({ plmn: '23415', regionCode: 'GB' })
    expect(wrapper.get('img').attributes('src')).toBe(carrierLogoURL('23415'))
    expect(wrapper.find('.fi-gb').exists()).toBe(true)
    await wrapper.get('img').trigger('load')
    await wrapper.setProps({ icon: 'data:image/png;base64,new' })
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,new')
    expect(wrapper.find('.fi-gb').exists()).toBe(true)
  })

  it('keeps a loaded embedded icon visible when only the carrier metadata changes', async () => {
    const wrapper = mount(CarrierLogo, {
      props: {
        name: 'Carrier',
        icon: 'data:image/png;base64,same',
        plmn: '310070',
        regionCode: 'US',
      },
    })
    await wrapper.get('img').trigger('load')
    const image = wrapper.get('img').element
    await wrapper.setProps({ plmn: '23415', regionCode: 'GB' })
    expect(wrapper.get('img').element).toBe(image)
    expect(wrapper.get('img').isVisible()).toBe(true)
    expect(wrapper.find('.fi-gb').exists()).toBe(false)
  })

  it.each(['', '310-070', '3107', '3100700', '../logo', '３１００７０'])(
    'skips invalid PLMN %s',
    (plmn) => {
      expect(carrierLogoURL(plmn)).toBe('')
      const wrapper = mount(CarrierLogo, { props: { name: 'Carrier', plmn, regionCode: 'US' } })
      expect(wrapper.find('img').exists()).toBe(false)
    },
  )

  it('preserves two and three digit MNCs and never pads them', () => {
    expect(profilePLMN({ mcc: '310', mnc: '070' })).toBe('310070')
    expect(profilePLMN({ mcc: '310', mnc: '70' })).toBe('31070')
    expect(profilePLMN({ mcc: '31', mnc: '0070' })).toBe('')
  })
})
