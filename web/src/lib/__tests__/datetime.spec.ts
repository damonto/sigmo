import { describe, expect, it } from 'vitest'

import { formatMessageTimestamp } from '@/lib/datetime'

describe('formatMessageTimestamp', () => {
  const now = new Date('2026-05-08T20:00:00')

  const tests = [
    {
      name: 'same local day shows 12-hour time',
      value: '2026-05-08T09:05:00',
      locales: 'en-US',
      want: '9:05 AM',
    },
    {
      name: 'same year shows short month and day',
      value: '2026-01-22T12:00:00',
      locales: 'en-US',
      want: 'Jan 22',
    },
    {
      name: 'different year shows day month and year',
      value: '2024-12-14T12:00:00',
      locales: 'en-GB',
      want: '14 Dec 2024',
    },
    {
      name: 'empty value stays empty',
      value: '',
      locales: 'en-US',
      want: '',
    },
    {
      name: 'invalid value is preserved',
      value: 'not-a-date',
      locales: 'en-US',
      want: 'not-a-date',
    },
  ]

  for (const tt of tests) {
    it(tt.name, () => {
      expect(formatMessageTimestamp(tt.value, tt.locales, now)).toBe(tt.want)
    })
  }
})
