import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RegionFlag from '@/components/RegionFlag.vue'

describe('RegionFlag', () => {
  it.each([
    { name: 'uppercase region', regionCode: 'US' },
    { name: 'lowercase region', regionCode: 'us' },
  ])('renders flag icon for $name', ({ regionCode }) => {
    const wrapper = mount(RegionFlag, {
      props: { regionCode },
      attrs: { class: 'text-xl' },
    })

    const flag = wrapper.find('span')
    expect(flag.classes()).toContain('fi')
    expect(flag.classes()).toContain('fi-us')
    expect(flag.classes()).toContain('text-xl')
    expect(flag.attributes('aria-label')).toBe('US')
  })

  it('renders region code when the value is not a two-letter flag code', () => {
    const wrapper = mount(RegionFlag, {
      props: { regionCode: 'unknown' },
    })

    expect(wrapper.text()).toBe('UNKNOWN')
    expect(wrapper.find('span').classes()).not.toContain('fi')
  })

  it('renders nothing when the region code is empty', () => {
    const wrapper = mount(RegionFlag, {
      props: { regionCode: '  ' },
    })

    expect(wrapper.find('span').exists()).toBe(false)
    expect(wrapper.text()).toBe('')
  })
})
