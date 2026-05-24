export type HomeModemItem = {
  id: string
  name: string
  regionCode: string
  operatorName: string
  registeredOperatorName: string
  registeredOperatorCode: string
  registrationState: string
  accessTechnology: string | null
  supportsEsim: boolean
  number: string
  signalQuality: number
  wifiCallingConnected: boolean
}
