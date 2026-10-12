export type AuthVerifyResponse = {
  token: string
}

export type AuthOtpRequirementResponse = {
  otpRequired: boolean
}

export type AuthVerifyPayload = {
  code: string
}
