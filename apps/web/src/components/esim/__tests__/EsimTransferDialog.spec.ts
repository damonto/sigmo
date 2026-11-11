import { ref } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import EsimLineItem from '@/components/esim/EsimLineItem.vue'
import EsimTransferDialog from '@/components/esim/EsimTransferDialog.vue'
import EsimTransferProfileStep from '@/components/esim/EsimTransferProfileStep.vue'
import EsimTransferSourceItem from '@/components/esim/EsimTransferSourceItem.vue'
import EsimTransferStartConfirmAlert from '@/components/esim/EsimTransferStartConfirmAlert.vue'
import type { EsimTransferProfile } from '@/types/esim'

enableAutoUnmount(afterEach)

const api = vi.hoisted(() => ({
  getTransferSources: vi.fn(),
  getTransferProfiles: vi.fn(),
}))
vi.mock('@/apis/esim', () => ({ useEsimApi: () => api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const inactive: EsimTransferProfile = {
  id: 'inactive',
  type: 'esim',
  name: 'Inactive transferable line',
  iccid: '8901',
  enabled: false,
  supported: true,
}
const unsupported: EsimTransferProfile = {
  ...inactive,
  id: 'unsupported',
  name: 'Unsupported line',
  supported: false,
}
const passthrough = { template: '<div><slot /></div>' }

async function openTransfer(initiallyOpen = false) {
  const wrapper = mount(EsimTransferDialog, {
    props: { modemId: 'target', targetSeId: 'se-a', open: initiallyOpen },
    global: {
      stubs: {
        Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
        DialogFooter: passthrough,
        EsimPersistentDialogContent: passthrough,
        EsimTransferHeader: true,
        EsimDownloadProgressModal: true,
        EsimDownloadResultModal: true,
        EsimSourceDeletionAlert: true,
        CarrierWebsheetDialog: true,
        EsimTransferStartConfirmAlert: true,
      },
    },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

async function openProfiles(profiles: EsimTransferProfile[]) {
  api.getTransferProfiles.mockResolvedValueOnce({ data: ref(profiles) })
  const wrapper = await openTransfer()
  await wrapper.getComponent(EsimTransferSourceItem).trigger('click')
  await flushPromises()
  return wrapper
}

describe('EsimTransferDialog', () => {
  afterEach(() => vi.unstubAllGlobals())
  beforeEach(() => {
    vi.resetAllMocks()
    api.getTransferSources.mockResolvedValue({
      data: ref({
        sources: [{ type: 'modem', id: 'source', name: 'Source', requiresSourceImei: false }],
      }),
    })
  })

  it('loads sources when initially mounted open', async () => {
    const wrapper = await openTransfer(true)
    expect(api.getTransferSources).toHaveBeenCalledExactlyOnceWith('target')
    expect(wrapper.findComponent(EsimTransferSourceItem).exists()).toBe(true)
  })

  it('hides unsupported lines and requires manual selection of a supported line', async () => {
    const wrapper = await openProfiles([unsupported, inactive])
    const rows = wrapper.findAllComponents(EsimLineItem)
    expect(rows.map((row) => row.props('profile').id)).toEqual(['inactive'])
    expect(wrapper.text()).not.toContain(unsupported.name)
    const start = wrapper.get('button.order-1')
    expect(start.attributes('disabled')).toBeDefined()

    wrapper.getComponent(EsimTransferProfileStep).vm.$emit('select-profile', unsupported.id)
    await flushPromises()
    expect(start.attributes('disabled')).toBeDefined()

    await wrapper.getComponent(EsimLineItem).trigger('click')
    expect(start.attributes('disabled')).toBeUndefined()
    expect(wrapper.getComponent(EsimLineItem).props('selected')).toBe(true)
  })

  it.each([
    { name: 'all unsupported', profiles: [unsupported] },
    { name: 'empty response', profiles: [] },
  ])('keeps the empty profile page for $name', async ({ profiles }) => {
    const wrapper = await openProfiles(profiles)
    expect(wrapper.findAllComponents(EsimLineItem)).toHaveLength(0)
    expect(wrapper.text()).toContain('modemDetail.esim.transferNoProfiles')
    expect(wrapper.get('button.order-1').attributes('disabled')).toBeDefined()
    expect(wrapper.findComponent(EsimTransferProfileStep).exists()).toBe(true)
    expect(wrapper.findComponent(EsimTransferSourceItem).exists()).toBe(false)
    expect(wrapper.get('button.order-2').attributes('disabled')).toBeUndefined()
    await wrapper
      .get('button[aria-label="modemDetail.esim.transferChangeSourceLabel"]')
      .trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(EsimTransferSourceItem).exists()).toBe(true)
    expect(wrapper.props('open')).toBe(true)
  })

  it('returns to source selection without closing and requires a new profile choice', async () => {
    const wrapper = await openProfiles([inactive])
    await wrapper.getComponent(EsimLineItem).trigger('click')
    expect(wrapper.get('button.order-1').attributes('disabled')).toBeUndefined()
    await wrapper
      .get('button[aria-label="modemDetail.esim.transferChangeSourceLabel"]')
      .trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(EsimTransferProfileStep).exists()).toBe(false)
    expect(wrapper.findComponent(EsimTransferSourceItem).exists()).toBe(true)
    expect(wrapper.emitted('update:open')).toBeUndefined()
    expect(wrapper.props('targetSeId')).toBe('se-a')
    expect(api.getTransferSources).toHaveBeenCalledTimes(2)

    api.getTransferProfiles.mockResolvedValueOnce({ data: ref([inactive]) })
    await wrapper.getComponent(EsimTransferSourceItem).trigger('click')
    await flushPromises()
    expect(wrapper.get('button.order-1').attributes('disabled')).toBeDefined()
    expect(wrapper.getComponent(EsimLineItem).props('selected')).toBe(false)
  })

  it('can change source while profiles are loading and ignores the abandoned result', async () => {
    let resolveProfiles:
      ((value: { data: ReturnType<typeof ref<EsimTransferProfile[]>> }) => void) | undefined
    const pending = new Promise<{ data: ReturnType<typeof ref<EsimTransferProfile[]>> }>(
      (resolve) => {
        resolveProfiles = resolve
      },
    )
    api.getTransferProfiles.mockReturnValueOnce(pending)
    const wrapper = await openTransfer()
    await wrapper.getComponent(EsimTransferSourceItem).trigger('click')
    expect(wrapper.getComponent(EsimTransferProfileStep).props('loading')).toBe(true)
    await wrapper
      .get('button[aria-label="modemDetail.esim.transferChangeSourceLabel"]')
      .trigger('click')
    await flushPromises()
    if (!resolveProfiles) throw new Error('profile response callback was not initialized')
    resolveProfiles({ data: ref([inactive]) })
    await flushPromises()

    expect(wrapper.findComponent(EsimTransferSourceItem).exists()).toBe(true)
    expect(wrapper.findComponent(EsimTransferProfileStep).exists()).toBe(false)
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })

  it('can change source after a profile read fails', async () => {
    api.getTransferProfiles.mockRejectedValueOnce(new Error('Source is busy'))
    const wrapper = await openTransfer()
    await wrapper.getComponent(EsimTransferSourceItem).trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Source is busy')
    await wrapper
      .get('button[aria-label="modemDetail.esim.transferChangeSourceLabel"]')
      .trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(EsimTransferSourceItem).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Source is busy')
  })

  it('does not offer source changes once transfer is confirmed', async () => {
    vi.stubGlobal(
      'WebSocket',
      class {
        static OPEN = 1
        readyState = 0
        close() {}
        send() {}
      },
    )
    const wrapper = await openProfiles([inactive])
    await wrapper.getComponent(EsimLineItem).trigger('click')
    await wrapper.get('button.order-1').trigger('click')
    expect(
      wrapper.find('button[aria-label="modemDetail.esim.transferChangeSourceLabel"]').exists(),
    ).toBe(false)
    wrapper.getComponent(EsimTransferStartConfirmAlert).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.findComponent(EsimTransferProfileStep).exists()).toBe(false)
  })
})
