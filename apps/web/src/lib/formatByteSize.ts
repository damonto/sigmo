const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']

/** Formats a byte count in binary units, or N/A when no valid count is available. */
export const formatByteSize = (bytes?: number) => {
  if (bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return 'N/A'

  const unit = Math.min(Math.floor(Math.log2(Math.max(bytes, 1)) / 10), UNITS.length - 1)
  const amount = Math.round((bytes / 1024 ** unit) * 100) / 100
  return `${amount} ${UNITS[unit]}`
}
