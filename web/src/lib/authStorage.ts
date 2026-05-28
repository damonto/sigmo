const TOKEN_STORAGE_KEY = 'sigmo:token'

const hasStorage = () => typeof localStorage !== 'undefined'

export const getStoredToken = (): string | null => {
  if (!hasStorage()) return null

  const token = localStorage.getItem(TOKEN_STORAGE_KEY)
  if (token && token.trim().length > 0) {
    return token
  }

  return null
}

export const setStoredToken = (token: string) => {
  if (!hasStorage()) return
  localStorage.setItem(TOKEN_STORAGE_KEY, token)
}

export const clearStoredToken = () => {
  if (!hasStorage()) return
  localStorage.removeItem(TOKEN_STORAGE_KEY)
}
