const retryDelays = [1000, 2000, 4000] as const
const steadyRetryDelay = 10_000

export const createSIMKindRetry = () => {
  let attempt = 0
  let timer: ReturnType<typeof setTimeout> | null = null

  const reset = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    attempt = 0
  }

  const schedule = (pending: boolean, retry: () => void) => {
    if (!pending) {
      reset()
      return
    }
    if (timer !== null) return

    const delay = retryDelays[attempt] ?? steadyRetryDelay
    attempt = Math.min(attempt + 1, retryDelays.length)
    timer = setTimeout(() => {
      timer = null
      retry()
    }, delay)
  }

  return { reset, schedule }
}
