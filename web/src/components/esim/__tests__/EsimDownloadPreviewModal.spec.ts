import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import EsimDownloadPreviewModal from '@/components/esim/EsimDownloadPreviewModal.vue'
import type { EsimDownloadPreview } from '@/types/esim'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const profile: EsimDownloadPreview = {
  iccid: '8901000000000000001',
  serviceProviderName: 'Carrier',
  profileName: 'Travel Line',
  profileNickname: 'Travel',
  profileState: 'disabled',
  profileOwner: { mcc: '208', mnc: '09' },
}

const stubs = {
  Button: {
    props: ['type'],
    template: '<button v-bind="$attrs" :type="type || \'button\'"><slot /></button>',
  },
  Card: { template: '<div><slot /></div>' },
  CardContent: { template: '<div><slot /></div>' },
  Dialog: { template: '<div><slot /></div>' },
  DialogDescription: { template: '<p><slot /></p>' },
  DialogFooter: { template: '<footer><slot /></footer>' },
  DialogHeader: { template: '<header><slot /></header>' },
  DialogTitle: { template: '<h2><slot /></h2>' },
  EsimPersistentDialogContent: { template: '<section><slot /></section>' },
  RegionFlag: { template: '<span />' },
}

const mountModal = () =>
  mount(EsimDownloadPreviewModal, {
    props: {
      open: true,
      title: 'Preview',
      hint: 'Confirm',
      profile,
      confirmLabel: 'Confirm',
      cancelLabel: 'Cancel',
    },
    global: {
      stubs,
    },
  })

describe('EsimDownloadPreviewModal', () => {
  it('toggles provider, profile name, and owner details', async () => {
    const wrapper = mountModal()

    expect(wrapper.text()).not.toContain('modemDetail.esim.serviceProvider')

    await wrapper.get('button').trigger('click')

    expect(wrapper.text()).toContain('modemDetail.esim.serviceProvider')
    expect(wrapper.text()).toContain('Carrier')
    expect(wrapper.text()).toContain('modemDetail.esim.profileName')
    expect(wrapper.text()).toContain('Travel Line')
    expect(wrapper.text()).toContain('modemDetail.esim.profileOwner')
    expect(wrapper.text()).toContain('208')
    expect(wrapper.text()).toContain('09')

    await wrapper.get('button').trigger('click')

    expect(wrapper.text()).not.toContain('modemDetail.esim.serviceProvider')
  })
})
