import { effectScope, ref } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'

import { useEsimTransferRequestSelection } from '@/composables/useEsimTransferRequestSelection'
import type { EsimTransferRequest } from '@/types/esimPeer'

const scopes: ReturnType<typeof effectScope>[] = []
afterEach(() => {
  for (const scope of scopes) scope.stop()
  scopes.length = 0
})
const setup = () => {
  const profile = {
    id: 'e1',
    type: 'esim' as const,
    name: 'Line',
    iccid: '8901',
    enabled: false,
    supported: true,
  }
  const request = ref<EsimTransferRequest>({
    id: 'r1',
    phase: 'authorization',
    revision: 1,
    remoteAddress: '192.0.2.24',
    target: { name: 'Target', imei: '1'.repeat(15), eid: '2'.repeat(32) },
    expiresAt: '2099-01-01T00:00:00Z',
    sources: [
      {
        source: { type: 'modem', id: 'm1', name: 'Modem', requiresSourceImei: false },
        profiles: [
          profile,
          { ...profile, id: 'physical', type: 'physical' },
          { ...profile, id: 'unsupported', supported: false },
        ],
      },
      {
        source: { type: 'ccid', id: 'r1', name: 'Reader', requiresSourceImei: true },
        profiles: [profile],
      },
    ],
  })
  const scope = effectScope()
  scopes.push(scope)
  const selection = scope.run(() => useEsimTransferRequestSelection(request))
  if (!selection) throw new Error('missing selection scope')
  return { request, ...selection }
}
describe('source owner selection', () => {
  it('requires an explicit supported profile and cannot select a different source’s profile', () => {
    const form = setup()
    expect(form.selection.value).toBeUndefined()
    form.sourceKey.value = 'modem:m1'
    expect(form.profiles.value.map((p) => p.id)).toEqual(['e1'])
    form.profileID.value = 'physical'
    expect(form.selection.value).toBeUndefined()
    form.profileID.value = 'e1'
    expect(form.selection.value).toMatchObject({
      sourceType: 'modem',
      sourceId: 'm1',
      profileId: 'e1',
    })
    form.sourceKey.value = 'modem:unknown'
    expect(form.profileID.value).toBe('')
    expect(form.selection.value).toBeUndefined()
  })
  it('validates the reader’s IMEI and resets it when another device is selected', () => {
    const form = setup()
    form.sourceKey.value = 'ccid:r1'
    form.profileID.value = 'e1'
    for (const value of ['', '12345', 'a'.repeat(15), '１'.repeat(15)]) {
      form.sourceImei.value = value
      expect(form.selection.value).toBeUndefined()
    }
    form.sourceImei.value = '1'.repeat(15)
    expect(form.selection.value?.sourceImei).toBe('1'.repeat(15))
    form.sourceKey.value = 'modem:m1'
    expect(form.profileID.value).toBe('')
    expect(form.sourceImei.value).toBe('')
  })
  it.each(['id', 'revision'] as const)(
    'clears choices immediately when the request %s changes',
    (field) => {
      const form = setup()
      form.sourceKey.value = 'modem:m1'
      form.profileID.value = 'e1'
      form.request.value =
        field === 'id'
          ? { ...form.request.value, id: 'r2' }
          : { ...form.request.value, revision: 2 }
      expect(form.sourceKey.value).toBe('')
      expect(form.selection.value).toBeUndefined()
    },
  )

  it('preserves the current choice when the same request snapshot is received again', () => {
    const form = setup()
    form.sourceKey.value = 'modem:m1'
    form.profileID.value = 'e1'
    const selection = form.selection.value
    form.request.value = { ...form.request.value }
    expect(form.selection.value).toEqual(selection)
  })

  it('never submits a selection for a request that no longer needs approval', () => {
    const form = setup()
    form.sourceKey.value = 'modem:m1'
    form.profileID.value = 'e1'
    form.request.value = { ...form.request.value, phase: 'running' }
    expect(form.selection.value).toBeUndefined()
  })
})
