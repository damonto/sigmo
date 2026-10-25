export type AppInfoResponse = {
  version: string
  commit: string
  channel: 'stable' | 'dev'
  target: string
  distribution: 'standalone' | 'container' | 'developer'
}
