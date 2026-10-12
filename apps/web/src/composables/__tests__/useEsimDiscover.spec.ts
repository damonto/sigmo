import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useEsimDiscover } from '@/composables/useEsimDiscover'

const api = vi.hoisted(() => ({
  discoverEsims: vi.fn(),
}))

vi.mock('@/apis/esim', () => ({
  useEsimApi: () => api,
}))

describe('useEsimDiscover', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('passes the selected SE to the discovery API', async () => {
    api.discoverEsims.mockResolvedValue({
      data: ref([{ address: 'smdp.example.com', confirmationCodeRequired: false }]),
    })
    const installDialogOpen = ref(true)
    const discover = useEsimDiscover({
      modemId: ref('modem-1'),
      installDialogOpen,
      applyDiscoverAddress: vi.fn(),
    })

    await discover.openDiscoverDialog('se1')

    expect(api.discoverEsims).toHaveBeenCalledWith('modem-1', 'se1')
    expect(installDialogOpen.value).toBe(false)
    expect(discover.discoverDialogOpen.value).toBe(true)
    expect(discover.discoverOptions.value).toEqual([
      { address: 'smdp.example.com', confirmationCodeRequired: false },
    ])
  })
})
