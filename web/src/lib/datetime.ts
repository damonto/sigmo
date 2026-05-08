const localDateParts = (date: Date) => ({
  year: date.getFullYear(),
  month: date.getMonth(),
  day: date.getDate(),
})

const isSameLocalDay = (left: Date, right: Date) => {
  const leftParts = localDateParts(left)
  const rightParts = localDateParts(right)

  return (
    leftParts.year === rightParts.year &&
    leftParts.month === rightParts.month &&
    leftParts.day === rightParts.day
  )
}

const browserLocales = (): Intl.LocalesArgument => {
  if (typeof navigator === 'undefined') return undefined
  if (navigator.languages.length > 0) return navigator.languages
  return navigator.language || undefined
}

export const formatMessageTimestamp = (
  value: string,
  locales: Intl.LocalesArgument = browserLocales(),
  now = new Date(),
) => {
  if (!value) return ''

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  if (isSameLocalDay(date, now)) {
    return new Intl.DateTimeFormat(locales, {
      hour: 'numeric',
      minute: '2-digit',
      hour12: true,
    }).format(date)
  }

  if (date.getFullYear() === now.getFullYear()) {
    return new Intl.DateTimeFormat(locales, {
      month: 'short',
      day: 'numeric',
    }).format(date)
  }

  return new Intl.DateTimeFormat(locales, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  }).format(date)
}
