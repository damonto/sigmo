import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import { expect, it, vi } from 'vitest'
import { useDeviceCallHistory } from '@/composables/useDeviceCallHistory'
import type { CallRecord } from '@/types/call'
const api = vi.hoisted(() => ({ listCalls: vi.fn(), deleteCall: vi.fn() }))
vi.mock('@/apis/call', () => ({ useCallApi: () => api }))

it('discards an earlier device response after switching devices', async () => {
  const modem = ref('a')
  let finish: ((value: { data: { value: CallRecord[] } }) => void) | undefined
  api.listCalls
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    .mockResolvedValueOnce({ data: { value: [] } })
  let history: ReturnType<typeof useDeviceCallHistory> | undefined
  const wrapper = mount(
    defineComponent({
      setup() {
        history = useDeviceCallHistory(computed(() => modem.value))
        return () => null
      },
    }),
  )
  modem.value = 'b'
  await flushPromises()
  finish?.({
    data: {
      value: [
        {
          callID: 'old',
          number: '10086',
          direction: 'outgoing',
          state: 'ended',
          hold: 'none',
          route: 'volte',
          reason: '',
          startedAt: '',
          endedAt: '',
          answeredAt: '',
          updatedAt: '',
        },
      ],
    },
  })
  await flushPromises()
  expect(api.listCalls).toHaveBeenCalledWith('b', '')
  expect(history?.recentCalls.value).toEqual([])
  wrapper.unmount()
})
