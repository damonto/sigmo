import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import HomeHeader from '@/components/home/HomeHeader.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const mountHeader = (version?: string) =>
  mount(HomeHeader, {
    props: {
      subtitle: 'home.subtitle',
      version,
      isLoading: false,
    },
    global: {
      stubs: {
        Button: {
          template: '<button type="button"><slot /></button>',
        },
        RouterLink: {
          template: '<a><slot /></a>',
        },
      },
    },
  })

describe('HomeHeader', () => {
  it.each([
    { name: 'with version', version: 'v1.2.3', wantVersion: true },
    { name: 'without version', version: undefined, wantVersion: false },
  ])('renders the title $name', ({ version, wantVersion }) => {
    const wrapper = mountHeader(version)

    expect(wrapper.text()).toContain('home.title')
    expect(wrapper.text().includes('v1.2.3')).toBe(wantVersion)
  })
})
