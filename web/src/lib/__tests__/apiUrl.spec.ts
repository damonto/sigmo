import { afterEach, describe, expect, it, vi } from 'vitest'

import { resolveAPIURL } from '@/lib/apiUrl'

describe('resolveAPIURL', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('resolves absolute API paths against the configured API origin', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://localhost:8080/api/v1')

    expect(resolveAPIURL('/api/v1/websheets/sheet-1')).toBe(
      'http://localhost:8080/api/v1/websheets/sheet-1',
    )
  })

  it('keeps absolute URLs unchanged', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://localhost:8080/api/v1')

    expect(resolveAPIURL('https://example.com/api/v1/websheets/sheet-1')).toBe(
      'https://example.com/api/v1/websheets/sheet-1',
    )
  })

  it('resolves relative API paths under the configured base path', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://localhost:8080/api/v1')

    expect(resolveAPIURL('websheets/sheet-1')).toBe(
      'http://localhost:8080/api/v1/websheets/sheet-1',
    )
  })
})
