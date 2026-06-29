import parsePhoneNumberFromString, {
  AsYouType,
  getCountryCallingCode,
  type CountryCode,
} from 'libphonenumber-js'

const shortCodeRE = /^\d{1,6}$/

export const phoneNumberChars = (value: string) => {
  let result = ''
  for (const char of value.trim()) {
    if (char >= '0' && char <= '9') {
      result += char
      continue
    }
    if (char === '+' && result.length === 0) {
      result += char
    }
  }
  return result
}

export const dialStringChars = (value: string) => {
  let result = ''
  for (const char of value.trim()) {
    if (char >= '0' && char <= '9') {
      result += char
      continue
    }
    if (char === '+' && result.length === 0) {
      result += char
      continue
    }
    if ((char === '*' || char === '#') && !result.startsWith('+')) {
      result += char
    }
  }
  return result
}

export const isDialServiceCode = (value: string) => {
  const chars = dialStringChars(value)
  return chars.startsWith('*') || chars.startsWith('#')
}

export const isCallableDialString = (value: string) => {
  const chars = dialStringChars(value)
  return chars !== '' && chars !== '+'
}

export const formatPhoneInput = (value: string, defaultCountry?: string) => {
  const chars = phoneNumberChars(value)
  if (!chars || chars === '+') return chars
  if (!shouldFormatPhoneChars(chars, defaultCountry)) return chars

  const country = phoneCountry(defaultCountry)
  if (!chars.startsWith('+') && !country) return chars

  const formatter = chars.startsWith('+') ? new AsYouType() : new AsYouType(country)
  return formatter.input(chars) || chars
}

export const formatDialInput = (value: string, defaultCountry?: string) => {
  const chars = dialStringChars(value)
  if (isDialServiceCode(chars)) return chars
  return formatPhoneInput(chars, defaultCountry)
}

export const formatAddressInput = (value: string, defaultCountry?: string) => {
  const address = addressSubmissionChars(value)
  if (!address.valid) return value.trim()
  return formatPhoneInput(address.value, defaultCountry)
}

export const formatPhoneDisplay = (value: string, defaultCountry?: string) => {
  const chars = phoneNumberChars(value)
  if (!chars || chars === '+') return value.trim()
  if (!shouldFormatPhoneChars(chars, defaultCountry)) return chars

  const country = phoneCountry(defaultCountry)
  const number = chars.startsWith('+')
    ? parsePhoneNumberFromString(chars)
    : country
      ? parsePhoneNumberFromString(chars, country)
      : undefined
  if (number?.isPossible()) {
    if (
      country &&
      (number.country === country || number.countryCallingCode === getCountryCallingCode(country))
    ) {
      return number.formatNational()
    }
    return chars.startsWith('+') ? number.formatInternational() : number.formatNational()
  }
  return formatPhoneInput(chars, defaultCountry) || value.trim()
}

export const normalizeAddressSubmission = (value: string) => {
  const address = addressSubmissionChars(value)
  return address.valid ? address.value : value.trim()
}

export const normalizePhoneSubmission = (value: string, defaultCountry?: string) => {
  const chars = phoneNumberChars(value)
  if (!chars || chars === '+') return ''
  if (shortCodeRE.test(chars)) return chars

  const country = phoneCountry(defaultCountry)
  if (chars.startsWith('+')) {
    const number = parsePhoneNumberFromString(chars)
    return number?.isPossible() ? number.number : chars
  }
  if (!country) return chars

  const defaultCallingCode = getCountryCallingCode(country)
  const parsed = parsePhoneNumberFromString(chars, country)
  if (parsed?.isPossible()) {
    if (parsed.number === `+${chars}`) return parsed.number
    if (parsed.countryCallingCode !== defaultCallingCode) return parsed.number
    return chars
  }

  const international = parsePhoneNumberFromString(`+${chars}`)
  if (international?.isPossible() && international.countryCallingCode !== defaultCallingCode) {
    return international.number
  }
  return chars
}

const phoneCountry = (value?: string): CountryCode | undefined => {
  const country = value?.trim().toUpperCase()
  if (!country || country === 'UN' || !/^[A-Z]{2}$/.test(country)) return undefined
  return country as CountryCode
}

const shouldFormatPhoneChars = (chars: string, defaultCountry?: string) => {
  if (shortCodeRE.test(chars)) return false
  if (chars.startsWith('+')) return true
  const country = phoneCountry(defaultCountry)
  if (!country) return false
  const number = parsePhoneNumberFromString(chars, country)
  return number?.isPossible() === true && phoneNumberChars(number.formatNational()) === chars
}

const addressSubmissionChars = (value: string) => {
  const trimmed = value.trim()
  let result = ''
  for (const char of trimmed) {
    if (char >= '0' && char <= '9') {
      result += char
      continue
    }
    if (char === '+' && result.length === 0) {
      result += char
      continue
    }
    if (char === ' ' || char === '-' || char === '.' || char === '(' || char === ')') {
      continue
    }
    return { value: trimmed, valid: false }
  }
  return { value: result, valid: result !== '' && result !== '+' }
}
