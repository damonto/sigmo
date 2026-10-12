import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useReminderApi } from '@/apis/reminder'

describe('useReminderApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  const payload = {
    scheduledAt: '2026-07-18T02:30:00.000Z',
    repeatDays: 7,
    content: 'Renew',
  }

  const tests = [
    {
      name: 'saves physical SIM reminder',
      run: () => useReminderApi().savePhysicalReminder('modem-1', '8985', payload),
      path: '/api/v1/modems/modem-1/sims/8985/reminder',
      method: 'PUT',
    },
    {
      name: 'clears physical SIM reminder',
      run: () => useReminderApi().deletePhysicalReminder('modem-1', '8985'),
      path: '/api/v1/modems/modem-1/sims/8985/reminder',
      method: 'DELETE',
    },
    {
      name: 'saves eSIM reminder',
      run: () => useReminderApi().saveEsimReminder('modem-1', 'se0', '8985', payload),
      path: '/api/v1/modems/modem-1/ses/se0/esims/8985/reminder',
      method: 'PUT',
    },
    {
      name: 'clears eSIM reminder',
      run: () => useReminderApi().deleteEsimReminder('modem-1', 'se0', '8985'),
      path: '/api/v1/modems/modem-1/ses/se0/esims/8985/reminder',
      method: 'DELETE',
    },
  ]

  for (const tt of tests) {
    it(tt.name, async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(tt.method === 'PUT' ? JSON.stringify(payload) : null, {
          status: tt.method === 'PUT' ? 200 : 204,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      vi.stubGlobal('fetch', fetchMock)

      await tt.run()

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining(tt.path),
        expect.objectContaining({
          method: tt.method,
          ...(tt.method === 'PUT' ? { body: JSON.stringify(payload) } : {}),
        }),
      )
    })
  }
})
