export type MCPPermission = {
  name: string
  module: string
}

export type MCPPermissionGroup = {
  module: string
  permissions: string[]
}

export type MCPSettings = {
  enabled: boolean
  endpointPath: string
  auditRetentionDays: number
  permissions: MCPPermission[]
}

export type MCPAPIKey = {
  id: string
  name: string
  tokenHint: string
  status: 'active' | 'expired' | 'revoked'
  allModems: boolean
  modemIds: string[]
  permissions: string[]
  createdAt: string
  expiresAt: string
  revokedAt?: string
}

export type CreateMCPAPIKey = {
  name: string
  validityDays: number
  allModems: boolean
  modemIds: string[]
  permissions: string[]
}

export type CreateMCPAPIKeyResponse = {
  apiKey: MCPAPIKey
  token: string
}

export type MCPAPIKeysResponse = {
  apiKeys: MCPAPIKey[]
}

export type MCPAuditEvent = {
  id: number
  keyId: string
  keyName: string
  tool: string
  modemIds: string[]
  outcome: 'success' | 'error' | 'cancelled'
  errorCode?: string
  durationMs: number
  createdAt: string
}

export type MCPAuditEventsResponse = {
  events: MCPAuditEvent[]
  nextCursor?: number
}
