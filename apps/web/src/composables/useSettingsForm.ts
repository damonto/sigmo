import { computed, ref, type Ref } from 'vue'

import type {
  SettingsChannel,
  SettingsChannelSchema,
  SettingsResponse,
  SettingsValues,
} from '@/types/settings'

export type SettingsRootSection = 'proxy'

export const useSettingsForm = (
  settings: Ref<SettingsResponse | null>,
  values: Ref<SettingsValues | null>,
) => {
  const expandedChannels = ref<Record<string, boolean>>({})

  const schema = computed(() => settings.value?.schema)
  const authFields = computed(() => schema.value?.auth ?? [])
  const proxyFields = computed(() => schema.value?.proxy ?? [])
  const channelSchemas = computed(() => schema.value?.channels ?? [])
  const configuredChannelSchemas = computed(() =>
    channelSchemas.value.filter((channel) => Boolean(settings.value?.values.channels[channel.key])),
  )
  const isReady = computed(() => values.value !== null && schema.value !== undefined)
  const authValues = computed(() => values.value?.auth ?? null)
  const proxyValues = computed(() => values.value?.proxy ?? null)
  const channels = computed(() => values.value?.channels ?? {})

  const rootRecord = (section: SettingsRootSection) => {
    if (!values.value) return null
    return values.value[section] as unknown as Record<string, unknown>
  }

  const setRootValue = (section: SettingsRootSection, key: string, value: unknown) => {
    const record = rootRecord(section)
    if (!record) return
    record[key] = value
  }

  const setAuthValue = (key: string, value: unknown) => {
    const auth = values.value?.auth
    if (!auth) return
    if (
      key === 'mode' &&
      (value === 'none' || value === 'otp' || value === 'oidc' || value === 'cloudflare_access')
    ) {
      auth.mode = value
    } else if (key === 'sessionMaxDays' && typeof value === 'number') auth.sessionMaxDays = value
    else if (
      ['otpChannels', 'oidcAllowedSubjects', 'accessAllowedSubjects'].includes(key) &&
      Array.isArray(value) &&
      value.every((item): item is string => typeof item === 'string')
    ) {
      switch (key) {
        case 'otpChannels':
          auth.otpChannels = value
          break
        case 'oidcAllowedSubjects':
          auth.oidcAllowedSubjects = value
          break
        case 'accessAllowedSubjects':
          auth.accessAllowedSubjects = value
          break
      }
    } else if (typeof value === 'string') {
      switch (key) {
        case 'oidcIssuer':
          auth.oidcIssuer = value
          break
        case 'oidcClientId':
          auth.oidcClientId = value
          break
        case 'oidcClientSecret':
          auth.oidcClientSecret = value
          break
        case 'accessTeamDomain':
          auth.accessTeamDomain = value
          break
        case 'accessAudience':
          auth.accessAudience = value
          break
      }
    }
  }

  const setChannelValue = (channel: string, key: string, value: unknown) => {
    if (!values.value?.channels[channel]) return
    const record = values.value.channels[channel] as Record<string, unknown>
    record[key] = value
  }

  const defaultChannel = (schema: SettingsChannelSchema): SettingsChannel => {
    const channel: Record<string, unknown> = {}
    for (const field of schema.fields) {
      if (field.control === 'switch') channel[field.key] = false
      if (field.control === 'number') channel[field.key] = 0
      if (field.control === 'list') channel[field.key] = []
      if (field.control === 'keyValue') channel[field.key] = {}
      if (field.control === 'select') channel[field.key] = field.options?.[0]?.value ?? ''
    }
    return channel as SettingsChannel
  }

  const initializeMissingChannels = () => {
    if (!values.value) return
    for (const channel of channelSchemas.value) {
      values.value.channels[channel.key] ??= defaultChannel(channel)
    }
  }

  const initializeExpandedChannels = () => {
    initializeMissingChannels()
    const firstConfigured = configuredChannelSchemas.value[0]?.key
    expandedChannels.value = Object.fromEntries(
      channelSchemas.value.map((channel) => [channel.key, channel.key === firstConfigured]),
    )
  }

  const isChannelExpanded = (channel: string) => {
    return expandedChannels.value[channel] === true
  }

  const toggleChannelDetails = (channel: string) => {
    expandedChannels.value = {
      ...expandedChannels.value,
      [channel]: !isChannelExpanded(channel),
    }
  }

  return {
    authFields,
    authValues,
    channels,
    channelSchemas,
    configuredChannelSchemas,
    expandedChannels,
    initializeExpandedChannels,
    isReady,
    proxyFields,
    proxyValues,
    setAuthValue,
    setChannelValue,
    setRootValue,
    toggleChannelDetails,
  }
}
