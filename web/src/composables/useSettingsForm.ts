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
  const enabledChannelSchemas = computed(() =>
    channelSchemas.value.filter(
      (channel) => settings.value?.values.channels[channel.key]?.enabled === true,
    ),
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
    if (!values.value) return
    const record = values.value.auth as unknown as Record<string, unknown>
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

  const toggleChannel = (schema: SettingsChannelSchema, enabled: boolean) => {
    if (!values.value) return
    const channel = values.value.channels[schema.key] ?? defaultChannel(schema, enabled)
    channel.enabled = enabled
    values.value.channels[schema.key] = channel
    if (enabled) {
      expandedChannels.value = { ...expandedChannels.value, [schema.key]: true }
    }
  }

  const defaultChannel = (schema: SettingsChannelSchema, enabled = true): SettingsChannel => {
    const channel: Record<string, unknown> = { enabled }
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
      values.value.channels[channel.key] ??= defaultChannel(channel, false)
    }
  }

  const initializeExpandedChannels = () => {
    initializeMissingChannels()
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
    enabledChannelSchemas,
    expandedChannels,
    initializeExpandedChannels,
    isReady,
    proxyFields,
    proxyValues,
    setAuthValue,
    setChannelValue,
    setRootValue,
    toggleChannel,
    toggleChannelDetails,
  }
}
