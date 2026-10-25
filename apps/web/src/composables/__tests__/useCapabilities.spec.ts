import { ref } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({ getCapabilities: vi.fn() }))
vi.mock('@/apis/capability', () => ({ useCapabilityApi: () => api }))
beforeEach(() => {
  vi.resetModules()
  vi.resetAllMocks()
})

it('waits for the shared capability request before either pane decides support', async () => {
  const { useCapabilities } = await import('@/composables/useCapabilities')
  let release: (() => void) | undefined
  api.getCapabilities.mockImplementation(async () => {
    await new Promise<void>((resolve) => {
      release = resolve
    })
    return { data: ref({ features: ['contacts', 'messageThreads'] }) }
  })
  const list = useCapabilities()
  const detail = useCapabilities()
  const readList = list.fetchCapabilities()
  let detailReady = false
  const readDetail = detail.fetchCapabilities().then(() => {
    detailReady = true
  })
  await Promise.resolve()
  expect(detailReady).toBe(false)
  expect(api.getCapabilities).toHaveBeenCalledTimes(1)
  release?.()
  await Promise.all([readList, readDetail])
  expect(list.hasFeature('messageThreads')).toBe(true)
  expect(detail.hasFeature('messageThreads')).toBe(true)
  expect(detail.isLoading.value).toBe(false)
})

it('refreshes again when authorization changes during an in-flight request', async () => {
  const { useCapabilities } = await import('@/composables/useCapabilities')
  let release: (() => void) | undefined
  api.getCapabilities
    .mockImplementationOnce(async () => {
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return { data: ref({ features: ['wifiCalling'] }) }
    })
    .mockResolvedValueOnce({ data: ref({ features: ['contacts'] }) })
  const capabilities = useCapabilities()
  const original = capabilities.fetchCapabilities()
  const refreshed = capabilities.fetchCapabilities(true)
  const shared = capabilities.fetchCapabilities(true)
  release?.()
  await Promise.all([original, refreshed, shared])
  expect(api.getCapabilities).toHaveBeenCalledTimes(2)
  expect(capabilities.hasFeature('wifiCalling')).toBe(false)
  expect(capabilities.hasFeature('contacts')).toBe(true)
  expect(capabilities.isLoading.value).toBe(false)
  await capabilities.fetchCapabilities()
  expect(api.getCapabilities).toHaveBeenCalledTimes(2)
})
