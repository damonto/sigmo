import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import CarrierWebsheetDialog from '@/components/CarrierWebsheetDialog.vue'

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
        Button: {
          template: '<button type="button"><slot /></button>',
        },
        Dialog: {
          template: '<div><slot /></div>',
        },
        DialogDescription: {
          template: '<div><slot /></div>',
        },
        DialogFooter: {
          template: '<div><slot /></div>',
        },
        DialogHeader: {
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

const mountLegacyDialog = () =>
  mount(CarrierWebsheetDialog, {
    props: {
      open: true,
      websheet,
      browserFrame: false,
      showFooter: true,
    },
    global: {
      stubs: {
        Button: {
          template: '<button type="button"><slot /></button>',
        },
        Dialog: {
          template: '<div><slot /></div>',
        },
        DialogDescription: {
          template: '<div><slot /></div>',
        },
        DialogFooter: {
          template: '<div><slot /></div>',
        },
        DialogHeader: {
          template: '<div><slot /></div>',
        },
        DialogTitle: {
          template: '<div><slot /></div>',
        },
        EsimPersistentDialogContent: {
          props: ['showCloseButton'],
          template: '<div :data-show-close-button="showCloseButton"><slot /></div>',
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

  it('loads the websheet iframe with auth token and carrier-compatible sandbox flags', () => {
    localStorage.setItem('sigmo:token', 'sigmo-token')

    const wrapper = mountDialog()
    const iframe = wrapper.get('iframe')
    const src = new URL(iframe.attributes('src') ?? '')

    expect(src.pathname).toBe('/api/v1/websheets/sheet-1')
    expect(src.searchParams.get('token')).toBe('sigmo-token')
    expect(iframe.attributes('sandbox')).toContain('allow-same-origin')
    expect(iframe.attributes('sandbox')).toContain('allow-popups-to-escape-sandbox')
  })

  it('uses the browser-style frame by default without footer actions', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).toContain('Carrier setup')
    expect(wrapper.text()).not.toContain('modemDetail.actions.cancel')
    expect(wrapper.text()).not.toContain('modemDetail.esim.carrierWebsheetDone')
    expect(wrapper.find('.bg-red-400').exists()).toBe(false)
    expect(wrapper.find('.bg-amber-400').exists()).toBe(false)
    expect(wrapper.find('.bg-emerald-400').exists()).toBe(false)
  })

  it('can still render the legacy footer when requested', () => {
    const wrapper = mountLegacyDialog()

    expect(wrapper.text()).toContain('modemDetail.actions.cancel')
    expect(wrapper.text()).toContain('modemDetail.esim.carrierWebsheetDone')
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
    expect(wrapper.emitted('done')).toHaveLength(1)
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
          DialogFooter: {
            template: '<div><slot /></div>',
          },
          DialogHeader: {
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
    expect(wrapper.emitted('done')).toHaveLength(1)
  })
})
