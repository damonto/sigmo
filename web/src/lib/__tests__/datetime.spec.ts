import { describe, expect, it } from 'vitest'

import {
  dateTimeLocalToISOString,
  formatDateTimeLocal,
  formatListTimestamp,
  formatMessageTimestamp,
  formatReminderTimestamp,
} from '@/lib/datetime'

describe('formatListTimestamp', () => {
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
      expect(formatListTimestamp(tt.value, tt.locales, now)).toBe(tt.want)
    })
  }

  it('keeps the message timestamp export on the shared formatter', () => {
    expect(formatMessageTimestamp('2026-05-08T09:05:00', 'en-US', now)).toBe('9:05 AM')
  })

  it('round trips a browser-local reminder time through UTC', () => {
    const localDate = new Date(2026, 6, 18, 10, 30, 0, 0)
    const iso = localDate.toISOString()

    expect(formatDateTimeLocal(iso)).toBe('2026-07-18T10:30')
    expect(dateTimeLocalToISOString('2026-07-18T10:30')).toBe(iso)
  })

  it('keeps the reminder date and minute in browser-local time', () => {
    const localDate = new Date(2026, 6, 18, 10, 30, 0, 0)

    expect(formatReminderTimestamp(localDate.toISOString(), 'en-US')).toBe(
      'Jul 18, 2026, 10:30 AM',
    )
  })
})
