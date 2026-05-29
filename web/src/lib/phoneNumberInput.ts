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

export const formatPhoneInput = (value: string, defaultCountry?: string) => {
  const chars = phoneNumberChars(value)
  if (!chars || chars === '+') return chars
  if (shortCodeRE.test(chars)) return chars

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

export const formatPhoneDisplay = (value: string, defaultCountry?: string) => {
  const chars = phoneNumberChars(value)
  if (!chars || chars === '+') return value.trim()
  if (shortCodeRE.test(chars)) return chars

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

const phoneCountry = (value?: string): CountryCode | undefined => {
  const country = value?.trim().toUpperCase()
  if (!country || country === 'UN' || !/^[A-Z]{2}$/.test(country)) return undefined
  return country as CountryCode
}
