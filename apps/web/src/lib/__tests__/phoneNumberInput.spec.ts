import { describe, expect, it } from 'vitest'

import {
  dialStringChars,
  formatAddressInput,
  formatDialInput,
  formatPhoneDisplay,
  formatPhoneInput,
  isCallableDialString,
  isDialServiceCode,
  normalizeAddressSubmission,
  normalizePhoneSubmission,
  phoneNumberChars,
} from '@/lib/phoneNumberInput'

describe('phoneNumberInput', () => {
  it.each([
    { number: '64210647889', region: 'NZ', expected: '+64 210 647 889' },
    { number: '+64210647889', region: 'NZ', expected: '+64 210 647 889' },
    { number: '0210647889', region: 'NZ', expected: '+64 210 647 889' },
    { number: '447386042885', region: 'GB', expected: '+44 7386 042885' },
    { number: '07386042885', region: 'GB', expected: '+44 7386 042885' },
    { number: '12245556666', region: 'US', expected: '+1 (224) 555-6666' },
    { number: '2245556666', region: 'US', expected: '+1 (224) 555-6666' },
    { number: '+12245556666', region: 'US', expected: '+1 (224) 555-6666' },
    { number: '8613800000000', region: 'CN', expected: '+86 138 0000 0000' },
  ])('retains the calling code for $number in $region', ({ number, region, expected }) => {
    expect(formatPhoneDisplay(number, region)).toBe(expected)
  })

  it.each([
    'BANK123',
    'BANK2025550123',
    '1-800-FLOWERS',
    '*123#',
    '*13800000000#',
    '10086',
    '10690760295102',
  ])('preserves non-phone addresses and service codes: %s', (value) => {
    expect(formatPhoneDisplay(` ${value} `, 'US')).toBe(value)
  })

  it.each([undefined, '', 'UN', 'XX'])(
    'formats only explicit international numbers without a known region: %s',
    (region) => {
      expect(formatPhoneDisplay('+12025550123', region)).toBe('+1 (202) 555-0123')
      expect(formatPhoneDisplay('2025550123', region)).toBe('2025550123')
    },
  )

  it('formats international input when the number starts with plus', () => {
    expect(formatPhoneInput('+12223334444', 'US')).toBe('+1 222 333 4444')
    expect(formatPhoneInput('+8613344445555', 'US')).toBe('+86 133 4444 5555')
  })

  it('formats national input for the modem country', () => {
    expect(formatPhoneInput('2223334444', 'US')).toBe('(222) 333-4444')
    expect(formatPhoneInput('13344445555', 'CN')).toBe('133 4444 5555')
    expect(formatPhoneInput('07123456789', 'GB')).toBe('07123 456789')
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

  it('keeps service numbers and international access dial strings compact', () => {
    expect(formatAddressInput('10690760295102', 'CN')).toBe('10690760295102')
    expect(formatAddressInput('0118613800138000', 'US')).toBe('0118613800138000')
    expect(formatAddressInput('008613800138000', 'CN')).toBe('008613800138000')
    expect(formatAddressInput('*123#', 'CN')).toBe('*123#')
    expect(formatAddressInput('12583113788889999', 'CN')).toBe('12583113788889999')
    expect(formatDialInput('12583113788889999', 'CN')).toBe('12583113788889999')
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

  it('checks whether a dial string can be submitted as a call', () => {
    expect(isCallableDialString('+')).toBe(false)
    expect(isCallableDialString('')).toBe(false)
    expect(isCallableDialString('12583113788889999')).toBe(true)
    expect(isCallableDialString('0118613800138000')).toBe(true)
  })

  it('formats display values without API-provided display fields', () => {
    expect(formatPhoneDisplay('+8613344445555', 'US')).toBe('+86 133 4444 5555')
    expect(formatPhoneDisplay('+12223334444', 'US')).toBe('+1 (222) 333-4444')
    expect(formatPhoneDisplay('2223334444', 'US')).toBe('+1 (222) 333-4444')
    expect(formatPhoneDisplay('10690760295102', 'CN')).toBe('10690760295102')
    expect(formatPhoneDisplay('0118613800138000', 'US')).toBe('+86 138 0013 8000')
  })

  it('normalizes address submissions without inferring E.164', () => {
    expect(normalizeAddressSubmission('+86 138 0013 8000')).toBe('+8613800138000')
    expect(normalizeAddressSubmission('138 0013 8000')).toBe('13800138000')
    expect(normalizeAddressSubmission('(650) 253-0000')).toBe('6502530000')
    expect(normalizeAddressSubmission('106 90760295102')).toBe('10690760295102')
    expect(normalizeAddressSubmission('011 86 138 0013 8000')).toBe('0118613800138000')
    expect(normalizeAddressSubmission('0086 138 0013 8000')).toBe('008613800138000')
    expect(normalizeAddressSubmission('*123#')).toBe('*123#')
    expect(normalizeAddressSubmission('abc123')).toBe('abc123')
    expect(normalizeAddressSubmission('12x34')).toBe('12x34')
  })

  it('normalizes formatted national numbers for submission without adding plus', () => {
    expect(normalizePhoneSubmission('(222) 333-4444', 'US')).toBe('2223334444')
    expect(normalizePhoneSubmission('133 4444 5555', 'CN')).toBe('13344445555')
  })

  it('normalizes international numbers for submission with plus', () => {
    expect(normalizePhoneSubmission('+86 133 4444 5555', 'US')).toBe('+8613344445555')
    expect(normalizePhoneSubmission('011 86 133 4444 5555', 'US')).toBe('+8613344445555')
    expect(normalizePhoneSubmission('86 133 4444 5555', 'US')).toBe('+8613344445555')
    expect(normalizePhoneSubmission('1 224 225 5559', 'US')).toBe('+12242255559')
    expect(normalizePhoneSubmission('86 133 4444 5555', 'CN')).toBe('+8613344445555')
  })
})
