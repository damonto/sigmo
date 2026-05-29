import { describe, expect, it } from 'vitest'

import {
  dialStringChars,
  formatDialInput,
  formatPhoneDisplay,
  formatPhoneInput,
  isDialServiceCode,
  phoneNumberChars,
} from '@/lib/phoneNumberInput'

describe('phoneNumberInput', () => {
  it('formats international input when the number starts with plus', () => {
    expect(formatPhoneInput('+12223334444', 'US')).toBe('+1 222 333 4444')
    expect(formatPhoneInput('+8613344445555', 'US')).toBe('+86 133 4444 5555')
  })

  it('formats national input for the modem country', () => {
    expect(formatPhoneInput('2223334444', 'US')).toBe('(222) 333-4444')
    expect(formatPhoneInput('13344445555', 'CN')).toBe('133 4444 5555')
  })

  it('keeps input compact when there is no usable default country', () => {
    expect(formatPhoneInput('2223334444', 'UN')).toBe('2223334444')
  })

  it('keeps USSD and short service codes unformatted', () => {
    expect(formatDialInput('*123#', 'US')).toBe('*123#')
    expect(formatDialInput('10086', 'CN')).toBe('10086')
    expect(formatPhoneInput('10086', 'CN')).toBe('10086')
    expect(formatPhoneDisplay('10086', 'CN')).toBe('10086')
    expect(formatPhoneDisplay('911', 'US')).toBe('911')
    expect(isDialServiceCode('#123')).toBe(true)
  })

  it('extracts phone number characters from formatted input', () => {
    expect(phoneNumberChars('+1 (222) 333-4444')).toBe('+12223334444')
    expect(phoneNumberChars('(222) 333-4444')).toBe('2223334444')
    expect(phoneNumberChars('*123#')).toBe('123')
  })

  it('extracts dial string characters for the dialpad', () => {
    expect(dialStringChars('*123#')).toBe('*123#')
    expect(dialStringChars('+1 (222) 333-4444')).toBe('+12223334444')
  })

  it('formats display values without API-provided display fields', () => {
    expect(formatPhoneDisplay('+8613344445555', 'US')).toBe('+86 133 4444 5555')
    expect(formatPhoneDisplay('+12223334444', 'US')).toBe('(222) 333-4444')
    expect(formatPhoneDisplay('2223334444', 'US')).toBe('(222) 333-4444')
  })
})
