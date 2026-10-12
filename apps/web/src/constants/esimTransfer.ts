export const TRANSFER_STATE = {
  idle: 'idle',
  loadingSources: 'loading_sources',
  loadingProfiles: 'loading_profiles',
  ready: 'ready',
  connecting: 'connecting',
  progress: 'progress',
  userInput: 'user_input',
  sourceDeletion: 'source_deletion',
  websheet: 'websheet',
  completed: 'completed',
  error: 'error',
} as const

export type EsimTransferState = (typeof TRANSFER_STATE)[keyof typeof TRANSFER_STATE]

export const TRANSFER_MESSAGE = {
  start: 'start',
  progress: 'progress',
  preview: 'preview',
  userInput: 'user_input',
  sourceDeletion: 'source_deletion',
  websheet: 'websheet',
  completed: 'completed',
  error: 'error',
  cancel: 'cancel',
} as const

export const TRANSFER_STAGE = {
  preparing: 'preparing',
  carrier: 'carrier',
  websheet: 'websheet',
  downloading: 'downloading',
  enabling: 'enabling',
  completing: 'completing',
  deleting: 'deleting',
  authenticatingClient: 'Authenticating Client',
  authenticatingServer: 'Authenticating Server',
  installing: 'Installing',
} as const

export const TRANSFER_CLIENT_ERROR = {
  invalidResponse: 'invalid transfer response',
  connectionClosed: 'connection closed',
} as const

export const transferStageProgress: Record<string, number> = {
  [TRANSFER_STAGE.preparing]: 10,
  [TRANSFER_STAGE.carrier]: 25,
  [TRANSFER_STAGE.websheet]: 35,
  [TRANSFER_STAGE.downloading]: 45,
  [TRANSFER_STAGE.authenticatingClient]: 45,
  [TRANSFER_STAGE.authenticatingServer]: 60,
  [TRANSFER_STAGE.installing]: 75,
  [TRANSFER_STAGE.enabling]: 88,
  [TRANSFER_STAGE.deleting]: 90,
  [TRANSFER_STAGE.completing]: 95,
}

export const transferStageLabelKey: Record<string, string> = {
  [TRANSFER_STAGE.preparing]: 'modemDetail.esim.transferStagePreparing',
  [TRANSFER_STAGE.carrier]: 'modemDetail.esim.transferStageCarrier',
  [TRANSFER_STAGE.websheet]: 'modemDetail.esim.transferStageWebsheet',
  [TRANSFER_STAGE.downloading]: 'modemDetail.esim.transferStageDownloading',
  [TRANSFER_STAGE.authenticatingClient]: 'modemDetail.esim.downloadStageInitializing',
  [TRANSFER_STAGE.authenticatingServer]: 'modemDetail.esim.downloadStageConnecting',
  [TRANSFER_STAGE.installing]: 'modemDetail.esim.downloadStageInstalling',
  [TRANSFER_STAGE.enabling]: 'modemDetail.esim.transferStageEnabling',
  [TRANSFER_STAGE.completing]: 'modemDetail.esim.transferStageCompleting',
  [TRANSFER_STAGE.deleting]: 'modemDetail.esim.transferStageDeleting',
}
