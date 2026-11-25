export type ConnectionProgress = {
  attempt: number
  status: string
  stage: string
  stageStartedAt: string
  retryAt?: string
  steps: ConnectionProgressStep[]
  lastFailure?: {
    attempt: number
    stage: string
    code: string
    at: string
  }
}

export type ConnectionProgressStep = {
  stage: string
  status: string
  startedAt: string
}
