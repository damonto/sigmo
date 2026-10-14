import { ref } from 'vue'
import { expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({ getCapabilities: vi.fn() }))
vi.mock('@/apis/capability', () => ({ useCapabilityApi: () => api }))
import { useCapabilities } from '@/composables/useCapabilities'

it('waits for the shared capability request before either pane decides support', async () => {
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
