import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

class DocumentLocks {
  readonly held = new Set<string>()

  async request(
    name: string,
    options: LockOptions,
    callback: (lock: Lock | null) => Promise<void> | void,
  ) {
    expect(options.ifAvailable).toBe(true)
    if (this.held.has(name)) return callback(null)
    this.held.add(name)
    try {
      await callback({ name, mode: 'exclusive' })
    } finally {
      this.held.delete(name)
    }
  }
}

describe('call client identity', () => {
  beforeEach(() => {
    vi.resetModules()
    sessionStorage.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    sessionStorage.clear()
  })

  it('separates a duplicated tab while letting a destroyed document reload its identity', async () => {
    const locks = new DocumentLocks()
    vi.stubGlobal('navigator', { locks })
    sessionStorage.setItem('sigmo-call-client', 'saved-owner')
    const first = await import('@/lib/callClient')
    const preparation = first.prepareCallClient()
    expect(first.prepareCallClient()).toBe(preparation)
    await preparation
    expect(first.callClientId()).toBe('saved-owner')

    // Duplicating a tab copies sessionStorage, but does not release its lock.
    vi.resetModules()
    const duplicate = await import('@/lib/callClient')
    await duplicate.prepareCallClient()
    expect(duplicate.callClientId()).not.toBe(first.callClientId())
    expect(locks.held.size).toBe(2)

    // Model destruction of the original document, followed by its reload.
    locks.held.delete('sigmo-call-client:saved-owner')
    sessionStorage.setItem('sigmo-call-client', 'saved-owner')
    vi.resetModules()
    const reload = await import('@/lib/callClient')
    await reload.prepareCallClient()
    expect(reload.callClientId()).toBe('saved-owner')
  })

  it.each(['unsupported', 'denied'])(
    'does not reuse a cloned owner when locks are %s',
    async (mode) => {
      vi.stubGlobal('navigator', {
        locks:
          mode === 'unsupported'
            ? undefined
            : { request: () => Promise.reject(new Error('Denied')) },
      })
      sessionStorage.setItem('sigmo-call-client', 'cloned-owner')
      const client = await import('@/lib/callClient')
      await client.prepareCallClient()
      expect(client.callClientId()).not.toBe('cloned-owner')
      expect(client.callClientId()).toBe(client.callClientId())
    },
  )
})
