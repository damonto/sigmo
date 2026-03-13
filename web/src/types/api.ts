export type ApiErrorResponse = {
  error_code: string
  message: string
  request_id: string
}

export type EmptyObject = Record<string, never>
