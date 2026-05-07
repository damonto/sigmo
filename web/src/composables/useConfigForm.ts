import { computed, ref, type Ref } from 'vue'

import type {
  ConfigChannel,
  ConfigChannelSchema,
  ConfigResponse,
  ConfigValues,
} from '@/types/config'

export type ConfigRootSection = 'app' | 'proxy'
export type ConfigSectionKey = ConfigRootSection | 'channels'

const authFieldKeys = new Set(['otpRequired', 'authProviders'])

export const useConfigForm = (
  config: Ref<ConfigResponse | null>,
  values: Ref<ConfigValues | null>,
) => {
  const activeSection = ref<ConfigSectionKey>('app')
  const expandedChannels = ref<Record<string, boolean>>({})

  const schema = computed(() => config.value?.schema)
  const appFields = computed(() =>
    (schema.value?.app ?? []).filter((field) => !authFieldKeys.has(field.key)),
  )
  const authFields = computed(() =>
    (schema.value?.app ?? []).filter((field) => authFieldKeys.has(field.key)),
  )
  const proxyFields = computed(() => schema.value?.proxy ?? [])
  const channelSchemas = computed(() => schema.value?.channels ?? [])
  const enabledChannelSchemas = computed(() =>
    channelSchemas.value.filter((channel) => isChannelEnabled(channel.key)),
  )
  const isReady = computed(() => values.value !== null && schema.value !== undefined)
  const configPath = computed(() => config.value?.path ?? '')
  const appValues = computed(() => values.value?.app ?? null)
  const proxyValues = computed(() => values.value?.proxy ?? null)
  const channels = computed(() => values.value?.channels ?? {})

  const rootRecord = (section: ConfigRootSection) => {
    if (!values.value) return null
    return values.value[section] as unknown as Record<string, unknown>
  }

  const setRootValue = (section: ConfigRootSection, key: string, value: unknown) => {
    const record = rootRecord(section)
    if (!record) return
    record[key] = value
  }

  const setChannelValue = (channel: string, key: string, value: unknown) => {
    if (!values.value?.channels[channel]) return
    const record = values.value.channels[channel] as Record<string, unknown>
    record[key] = value
  }

  const isChannelEnabled = (channel: string) => {
    return values.value?.channels[channel]?.enabled === true
  }

  const toggleChannel = (schema: ConfigChannelSchema, enabled: boolean) => {
    if (!values.value) return
    const channel = values.value.channels[schema.key] ?? defaultChannel(schema, enabled)
    channel.enabled = enabled
    values.value.channels[schema.key] = channel
    if (!enabled) {
      removeAuthProvider(schema.key)
    }
    expandedChannels.value = { ...expandedChannels.value, [schema.key]: enabled }
  }

  const defaultChannel = (schema: ConfigChannelSchema, enabled = true): ConfigChannel => {
    const channel: Record<string, unknown> = { enabled }
    for (const field of schema.fields) {
      if (field.control === 'switch') channel[field.key] = false
      if (field.control === 'number') channel[field.key] = 0
      if (field.control === 'list') channel[field.key] = []
      if (field.control === 'keyValue') channel[field.key] = {}
      if (field.control === 'select') channel[field.key] = field.options?.[0]?.value ?? ''
    }
    return channel as ConfigChannel
  }

  const removeAuthProvider = (channel: string) => {
    if (!values.value) return
    values.value.app.authProviders = values.value.app.authProviders.filter(
      (item) => item !== channel,
    )
  }

  const initializeExpandedChannels = () => {
    const firstEnabled = channelSchemas.value.find((channel) => isChannelEnabled(channel.key))?.key
    expandedChannels.value = Object.fromEntries(
      channelSchemas.value.map((channel) => [
        channel.key,
        channel.key === firstEnabled && isChannelEnabled(channel.key),
      ]),
    )
  }

  const isChannelExpanded = (channel: string) => {
    return expandedChannels.value[channel] === true
  }

  const toggleChannelDetails = (channel: string) => {
    if (!isChannelEnabled(channel)) return
    expandedChannels.value = {
      ...expandedChannels.value,
      [channel]: !isChannelExpanded(channel),
    }
  }

  return {
    activeSection,
    appFields,
    appValues,
    authFields,
    channels,
    channelSchemas,
    configPath,
    enabledChannelSchemas,
    expandedChannels,
    initializeExpandedChannels,
    isReady,
    proxyFields,
    proxyValues,
    setChannelValue,
    setRootValue,
    toggleChannel,
    toggleChannelDetails,
  }
}
