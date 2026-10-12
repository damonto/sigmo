const defaultAPIBasePath = '/api/v1'

export const apiBaseUrl = () => {
  const rawBase = import.meta.env.VITE_API_BASE_URL as string | undefined
  return rawBase && rawBase.trim().length > 0 ? rawBase.replace(/\/$/, '') : defaultAPIBasePath
}

export const resolveAPIURL = (rawURL: string) => {
  const apiURL = new URL(apiBaseUrl(), window.location.origin)
  const base = rawURL.startsWith('/') ? apiURL.origin : `${apiURL.toString().replace(/\/$/, '')}/`
  return new URL(rawURL, base).toString()
}

export const resolveAPIWebSocketURL = (rawURL: string, token?: string | null) => {
  const apiURL = new URL(resolveAPIURL(rawURL))
  if (apiURL.protocol === 'https:') {
    apiURL.protocol = 'wss:'
  } else if (apiURL.protocol === 'http:') {
    apiURL.protocol = 'ws:'
  }
  const trimmedToken = token?.trim()
  if (trimmedToken) {
    apiURL.searchParams.set('token', trimmedToken)
  }
  return apiURL.toString()
}
