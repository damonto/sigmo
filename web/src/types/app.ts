export type AppInfoResponse = {
  version: string
  commit: string
  channel: 'stable' | 'dev'
  edition: 'community' | 'pro'
  target: string
  distribution: 'standalone' | 'container' | 'developer'
}
