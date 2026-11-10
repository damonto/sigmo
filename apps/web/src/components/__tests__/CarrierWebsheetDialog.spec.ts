import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import CarrierWebsheetDialog from '@/components/CarrierWebsheetDialog.vue'

enableAutoUnmount(afterEach)

const fetchJson = vi.hoisted(() => vi.fn())

vi.mock('@/lib/fetch', () => ({
  fetchJson,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const websheet = {
  id: 'sheet-1',
  embedUrl: '/api/v1/websheets/sheet-1',
  title: 'Carrier setup',
  url: 'https://example.com/setup',
  method: 'GET',
}

const mountDialog = () =>
  mount(CarrierWebsheetDialog, {
    props: {
      open: true,
      websheet,
    },
    global: {
      stubs: {
        Dialog: {
          template: '<div><slot /></div>',
        },
        DialogDescription: {
          template: '<div><slot /></div>',
        },
        DialogTitle: {
          template: '<div><slot /></div>',
        },
        EsimPersistentDialogContent: {
          template: '<div><slot /></div>',
        },
        Spinner: true,
      },
    },
  })

describe('CarrierWebsheetDialog', () => {
  beforeEach(() => {
    fetchJson.mockReset()
    fetchJson.mockResolvedValue({ data: { value: undefined } })
    localStorage.clear()
  })

  it('isolates carrier content without exposing the application session', () => {
    localStorage.setItem('sigmo:token', 'sigmo-token')

    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe')
    const src = new URL(iframe.attributes('src') ?? '')

    expect(src.pathname).toBe('/api/v1/websheets/sheet-1')
    expect(src.searchParams.has('token')).toBe(false)
    expect(iframe.attributes('sandbox')).not.toContain('allow-same-origin')
    expect(iframe.attributes('referrerpolicy')).toBe('no-referrer')
  })

  it('renders the browser-style frame and emits cancel from its close button', async () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).toContain('Carrier setup')
    const closeButton = wrapper.get('button')
    expect(closeButton.attributes('aria-label')).toBe('modemDetail.actions.cancel')
    await closeButton.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('relays terminal callbacks to the backend before closing the dialog state', async () => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    const callback = { source: 'vowifi', event: 'entitlementChanged', resultCode: 'success' }

    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'sigmo-websheet-callback', callback },
        source: iframe.contentWindow,
      }),
    )

    await vi.waitFor(() => {
      expect(fetchJson).toHaveBeenCalledWith('websheets/sheet-1/callback', {
        method: 'POST',
        body: JSON.stringify(callback),
      })
    })
    await vi.waitFor(() => expect(wrapper.emitted('done')).toHaveLength(1))
  })

  it('does not resubmit a callback acknowledged by the server bridge', async () => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    window.dispatchEvent(
      new MessageEvent('message', {
        source: iframe.contentWindow,
        data: {
          type: 'sigmo-websheet-callback',
          submitted: true,
          callback: { event: 'finishFlow' },
        },
      }),
    )
    await vi.waitFor(() => expect(wrapper.emitted('done')).toHaveLength(1))
    expect(fetchJson).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([null, [], {}, 'invalid', { event: 3 }])(
    'ignores malformed callbacks: %j',
    async (callback) => {
      const wrapper = mountDialog()
      const iframe = wrapper.get('iframe').element as HTMLIFrameElement
      window.dispatchEvent(
        new MessageEvent('message', {
          source: iframe.contentWindow,
          data: { type: 'sigmo-websheet-callback', submitted: true, callback },
        }),
      )
      await flushPromises()
      expect(fetchJson).not.toHaveBeenCalled()
      expect(wrapper.emitted('done')).toBeUndefined()
    },
  )

  it('emits completion once for repeated terminal notifications', async () => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    for (let index = 0; index < 2; index++) {
      window.dispatchEvent(
        new MessageEvent('message', {
          source: iframe.contentWindow,
          data: {
            type: 'sigmo-websheet-callback',
            submitted: true,
            callback: { event: 'finishFlow' },
          },
        }),
      )
    }
    await flushPromises()
    expect(wrapper.emitted('done')).toHaveLength(1)
    expect(fetchJson).not.toHaveBeenCalled()
  })

  it.each(['replace', 'close', 'unmount'])('ignores late delivery after %s', async (action) => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    let resolveDelivery: () => void = () => {}
    fetchJson.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveDelivery = resolve
      }),
    )
    window.dispatchEvent(
      new MessageEvent('message', {
        source: iframe.contentWindow,
        data: { type: 'sigmo-websheet-callback', callback: { event: 'finishFlow' } },
      }),
    )
    expect(fetchJson).toHaveBeenCalledOnce()
    if (action === 'replace') await wrapper.setProps({ websheet: { ...websheet, id: 'sheet-2' } })
    else if (action === 'close') await wrapper.setProps({ open: false })
    else wrapper.unmount()
    resolveDelivery()
    await flushPromises()
    expect(wrapper.emitted('done')).toBeUndefined()
  })

  it('keeps the page open when the bridge reports failed delivery', async () => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    window.dispatchEvent(
      new MessageEvent('message', {
        source: iframe.contentWindow,
        data: {
          type: 'sigmo-websheet-callback',
          submitted: false,
          callback: { event: 'finishFlow' },
        },
      }),
    )
    await Promise.resolve()
    expect(fetchJson).not.toHaveBeenCalled()
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })

  it('waits for legacy callback delivery and keeps the page open on failure', async () => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    let rejectDelivery: (reason: Error) => void = () => {}
    fetchJson.mockReturnValue(
      new Promise((_, reject) => {
        rejectDelivery = reject
      }),
    )
    const errorLog = vi.spyOn(console, 'error').mockImplementation(() => {})
    window.dispatchEvent(
      new MessageEvent('message', {
        source: iframe.contentWindow,
        data: { type: 'sigmo-websheet-callback', callback: { event: 'finishFlow' } },
      }),
    )
    expect(fetchJson).toHaveBeenCalledOnce()
    expect(wrapper.emitted('done')).toBeUndefined()
    rejectDelivery(new Error('offline'))
    await vi.waitFor(() => expect(errorLog).toHaveBeenCalledOnce())
    expect(wrapper.emitted('done')).toBeUndefined()
    errorLog.mockRestore()
    wrapper.unmount()
  })

  it('relays non-terminal callbacks without closing the dialog state', async () => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    const callback = { source: 'vowifi', method: 'phoneServicesAccountStatusChanged' }

    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'sigmo-websheet-callback', callback },
        source: iframe.contentWindow,
      }),
    )

    await vi.waitFor(() => {
      expect(fetchJson).toHaveBeenCalled()
    })
    expect(wrapper.emitted('done')).toBeUndefined()
  })

  it('can close on phone service status callbacks', async () => {
    const wrapper = mount(CarrierWebsheetDialog, {
      props: {
        open: true,
        websheet,
        closeOnStatusChange: true,
      },
      global: {
        stubs: {
          Dialog: {
            template: '<div><slot /></div>',
          },
          DialogDescription: {
            template: '<div><slot /></div>',
          },
          DialogTitle: {
            template: '<div><slot /></div>',
          },
          EsimPersistentDialogContent: {
            template: '<div><slot /></div>',
          },
          Spinner: true,
        },
      },
    })
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    const callback = { source: 'vowifi', method: 'phoneServicesAccountStatusChanged' }

    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'sigmo-websheet-callback', callback },
        source: iframe.contentWindow,
      }),
    )

    await vi.waitFor(() => {
      expect(fetchJson).toHaveBeenCalled()
    })
    await vi.waitFor(() => expect(wrapper.emitted('done')).toHaveLength(1))
  })

  it.each([
    'deleteToken',
    'checkProfileServiceStatus',
    'profileReadyWithActivationCode',
    'profileReadyWithDefaultSmdp',
  ])('keeps the carrier page open for the intermediate %s callback', async (event) => {
    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe').element as HTMLIFrameElement
    const callback = { source: 'odsa', event }

    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'sigmo-websheet-callback', callback },
        source: iframe.contentWindow,
      }),
    )

    await vi.waitFor(() => {
      expect(fetchJson).toHaveBeenCalledWith('websheets/sheet-1/callback', {
        method: 'POST',
        body: JSON.stringify(callback),
      })
    })
    expect(wrapper.emitted('done')).toBeUndefined()
    wrapper.unmount()
  })
})
