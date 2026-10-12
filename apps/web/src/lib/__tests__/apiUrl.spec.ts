import { afterEach, describe, expect, it, vi } from 'vitest'

import { resolveAPIURL, resolveAPIWebSocketURL } from '@/lib/apiUrl'

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

  it('resolves WebSocket URLs against the configured API base', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'https://sigmo.example/api/v1')

    expect(resolveAPIWebSocketURL('modems/modem-1/calls/events', 'token-1')).toBe(
      'wss://sigmo.example/api/v1/modems/modem-1/calls/events?token=token-1',
    )
  })

  it('keeps WebSocket query parameters when adding auth tokens', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://localhost:9527/api/v1')

    expect(resolveAPIWebSocketURL('modems/modem-1/events?scope=all', 'token-1')).toBe(
      'ws://localhost:9527/api/v1/modems/modem-1/events?scope=all&token=token-1',
    )
  })

  it('keeps absolute secure WebSocket URLs secure', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://localhost:9527/api/v1')

    expect(resolveAPIWebSocketURL('wss://relay.example/ws', 'token-1')).toBe(
      'wss://relay.example/ws?token=token-1',
    )
  })
})
