import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { communicationApi } from '@/apis/communication'
import { invalidateSession } from '@/lib/authSession'

const fetchJson = vi.hoisted(() => vi.fn())
vi.mock('@/lib/fetch', () => ({ fetchJson, useFetch: vi.fn() }))

beforeEach(() => {
  invalidateSession()
  fetchJson.mockReset()
  fetchJson.mockResolvedValue({ data: ref({ items: [] }) })
})

describe('communication directory requests', () => {
  it('shares ordinary reads and bypasses the cache for live handover candidates', async () => {
    await Promise.all([communicationApi.lines(), communicationApi.lines(true)])
    await communicationApi.lines(true)
    expect(fetchJson).toHaveBeenCalledTimes(1)
    expect(fetchJson).toHaveBeenCalledWith('sim-profiles?cached=true', undefined)
    await communicationApi.lines(false)
    expect(fetchJson).toHaveBeenLastCalledWith('sim-profiles', undefined)
  })
  it('invalidates after assignment and after a session change', async () => {
    await communicationApi.lines()
    await communicationApi.assign('old', 'new')
    await communicationApi.lines()
    invalidateSession()
    await communicationApi.lines()
    expect(
      fetchJson.mock.calls.filter(([path]) => path === 'sim-profiles?cached=true'),
    ).toHaveLength(3)
  })
  it('retries a failed read instead of caching the rejection', async () => {
    fetchJson.mockRejectedValueOnce(new Error('offline'))
    await expect(communicationApi.lines()).rejects.toThrow('offline')
    await communicationApi.lines()
    expect(fetchJson).toHaveBeenCalledTimes(2)
  })
})
