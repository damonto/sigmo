import { ref } from 'vue'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import EsimTransferRequestProvider from '@/components/esim/EsimTransferRequestProvider.vue'
import { Select } from '@/components/ui/select'
import en from '@/i18n/locales/en'
import type { EsimTransferRequest } from '@/types/esimPeer'

const actions = vi.hoisted(() => ({ decide: vi.fn(), dismiss: vi.fn() }))
const current = ref<EsimTransferRequest>()
const busy = ref(false)
const error = ref('')
vi.mock('@/composables/useEsimTransferRequests', () => ({
  useEsimTransferRequests: () => ({ current, busy, error, ...actions }),
}))
const profile = {
  id: 'profile-1',
  type: 'esim' as const,
  name: 'Travel line',
  iccid: '8901234567890123456',
  enabled: false,
  supported: true,
}
const request: EsimTransferRequest = {
  id: 'request-1',
  revision: 1,
  phase: 'authorization',
  remoteAddress: '192.0.2.24',
  target: {
    name: 'Target modem',
    imei: '123456789012345',
    eid: '12345678901234567890123456789012',
  },
  sources: [
    {
      source: { type: 'modem', id: 'source-1', name: 'Source reader', requiresSourceImei: false },
      profiles: [profile],
    },
  ],
  expiresAt: '2099-01-01T00:00:00Z',
}
let wrapper: ReturnType<typeof mount> | undefined
const mountProvider = async () => {
  wrapper = mount(EsimTransferRequestProvider, {
    attachTo: document.body,
    slots: { default: '<main>Settings page</main>' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] },
  })
  await flushPromises()
  const element = document.querySelector('[role="dialog"]')
  if (!element) throw new Error('missing request dialog')
  return new DOMWrapper(element)
}
const chooseProfile = async () => {
  if (!wrapper) throw new Error('missing provider')
  wrapper.findAllComponents(Select)[0]?.vm.$emit('update:modelValue', 'modem:source-1')
  await flushPromises()
  wrapper.findAllComponents(Select)[1]?.vm.$emit('update:modelValue', profile.id)
  await flushPromises()
}
beforeEach(() => {
  current.value = { ...request }
  busy.value = false
  error.value = ''
  vi.clearAllMocks()
})
afterEach(async () => {
  wrapper?.unmount()
  await flushPromises()
  document.body.innerHTML = ''
})

describe('source-owned eSIM transfer approval', () => {
  it('requires the source owner to select a device and profile before approval', async () => {
    const dialog = await mountProvider()
    expect(wrapper?.text()).toContain('Settings page')
    expect(dialog.get('h2').text()).toBe(en.esimPeer.sourceSelectionTitle)
    const approve = dialog.get<HTMLButtonElement>('[data-testid="approve-transfer-request"]')
    expect(approve.element.disabled).toBe(true)
    expect(actions.decide).not.toHaveBeenCalled()
    await chooseProfile()
    expect(approve.element.disabled).toBe(false)
    expect(dialog.text()).toContain('Travel line')
    expect(dialog.text()).toContain('Source reader')
    expect(dialog.text()).toContain(en.esimPeer.deletionConsent)
    await approve.trigger('click')
    expect(actions.decide).toHaveBeenCalledExactlyOnceWith(
      current.value,
      true,
      expect.objectContaining({ sourceType: 'modem', sourceId: 'source-1', profileId: profile.id }),
    )
  })
  it('keeps identifiers collapsed and resets selection for the next request', async () => {
    const dialog = await mountProvider()
    await chooseProfile()
    const details = dialog.get<HTMLDetailsElement>('details')
    expect(details.element.open).toBe(false)
    expect(details.text()).toContain(request.target.eid)
    expect(details.text()).toContain(profile.iccid)
    details.element.open = true
    current.value = { ...request, id: 'request-2' }
    await flushPromises()
    expect(dialog.get<HTMLDetailsElement>('details').element.open).toBe(false)
    expect(
      dialog.get<HTMLButtonElement>('[data-testid="approve-transfer-request"]').element.disabled,
    ).toBe(true)
  })
  it('allows rejection without choosing a profile and prevents accidental dismissal', async () => {
    const dialog = await mountProvider()
    await dialog.trigger('keydown', { key: 'Escape' })
    expect(actions.decide).not.toHaveBeenCalled()
    expect(actions.dismiss).not.toHaveBeenCalled()
    await dialog.get('[data-testid="reject-transfer-request"]').trigger('click')
    expect(actions.decide).toHaveBeenCalledWith(current.value, false)
  })
  it('disables choices during submission and reports decision errors', async () => {
    const dialog = await mountProvider()
    busy.value = true
    error.value = 'This request was already answered'
    await flushPromises()
    expect(dialog.findAll('button').every((button) => button.element.disabled)).toBe(true)
    expect(dialog.get('[role="alert"]').text()).toBe(error.value)
  })
  it('shows progress with no further approval or source selection', async () => {
    current.value = {
      ...request,
      phase: 'running',
      sourceName: 'Source reader',
      profile,
      sources: undefined,
    }
    const dialog = await mountProvider()
    expect(dialog.get('h2').text()).toBe(en.esimPeer.runningTitle)
    expect(dialog.findAll('ol li')).toHaveLength(3)
    expect(dialog.findAll('[role="combobox"]')).toHaveLength(0)
    expect(dialog.findAll('button')).toHaveLength(1)
    await dialog.get('button').trigger('click')
    expect(actions.dismiss).toHaveBeenCalledOnce()
    expect(actions.decide).not.toHaveBeenCalled()
  })
  it.each([
    ['completed', en.esimPeer.completedTitle],
    ['uncertain', en.esimPeer.uncertainTitle],
    ['failed', en.esimPeer.endedTitle],
    ['future-outcome', en.esimPeer.endedTitle],
  ])('shows %s as a terminal result with no reusable authorization', async (outcome, title) => {
    current.value = { ...request, phase: 'ended', outcome, sources: undefined, profile }
    const dialog = await mountProvider()
    expect(dialog.get('h2').text()).toBe(title)
    expect(dialog.text()).toContain(en.esimPeer.endedTitle)
    expect(dialog.findAll('button')).toHaveLength(1)
    if (outcome === 'uncertain') expect(dialog.text()).toContain(en.esimPeer.uncertain)
    await dialog.get('button').trigger('click')
    expect(actions.dismiss).toHaveBeenCalledOnce()
    expect(actions.decide).not.toHaveBeenCalled()
  })
})
