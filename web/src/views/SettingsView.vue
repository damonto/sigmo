<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'

import BackButton from '@/components/BackButton.vue'
import SettingsAuthSection from '@/components/settings/SettingsAuthSection.vue'
import SettingsChannelsSection from '@/components/settings/SettingsChannelsSection.vue'
import SettingsRootSection from '@/components/settings/SettingsRootSection.vue'
import SettingsSaveButton from '@/components/settings/SettingsSaveButton.vue'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useSettings } from '@/composables/useSettings'
import { useSettingsForm, type SettingsSectionKey } from '@/composables/useSettingsForm'

const { t } = useI18n()
const { settings, values, isLoading, isSaving, fetchSettings, saveSettings } = useSettings()
const {
  activeSection,
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
} = useSettingsForm(settings, values)

const sections = computed(() => [
  { key: 'auth' as const, label: t('settings.authTitle'), description: t('settings.authDescription') },
  {
    key: 'proxy' as const,
    label: t('settings.proxyTitle'),
    description: t('settings.proxyDescription'),
  },
  {
    key: 'channels' as const,
    label: t('settings.channelsTitle'),
    description: t('settings.channelsDescription'),
  },
])
const desktopMediaQuery = '(min-width: 768px)'
const scrollSpyOffset = 96
let isUnmounted = false
let removeScrollSpy: (() => void) | null = null

onMounted(async () => {
  await fetchSettings()
  if (isUnmounted) return

  initializeExpandedChannels()
  await nextTick()
  if (isUnmounted) return

  setupScrollSpy()
})

onUnmounted(() => {
  isUnmounted = true
  removeScrollSpy?.()
})

const handleSave = async () => {
  const response = await saveSettings()
  if (!response) return

  initializeExpandedChannels()
  toast.success(t('settings.saveSuccess'))
}

const sectionID = (section: SettingsSectionKey) => {
  return `settings-section-${section}`
}

const isDesktopViewport = () => {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia(desktopMediaQuery).matches
}

const isPageBottom = () => {
  const pageHeight = Math.max(document.documentElement.scrollHeight, document.body.scrollHeight)
  if (pageHeight <= window.innerHeight) return false
  return window.scrollY + window.innerHeight >= pageHeight - 1
}

const updateActiveSectionFromScroll = () => {
  if (!isDesktopViewport()) return

  if (isPageBottom()) {
    const lastSection = sections.value[sections.value.length - 1]
    if (lastSection) activeSection.value = lastSection.key
    return
  }

  let currentSection: SettingsSectionKey = 'auth'
  for (const section of sections.value) {
    const element = document.getElementById(sectionID(section.key))
    if (!element) continue
    if (element.getBoundingClientRect().top > scrollSpyOffset) break
    currentSection = section.key
  }
  activeSection.value = currentSection
}

const setupScrollSpy = () => {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return

  window.addEventListener('scroll', updateActiveSectionFromScroll, { passive: true })
  window.addEventListener('resize', updateActiveSectionFromScroll)
  updateActiveSectionFromScroll()

  removeScrollSpy = () => {
    window.removeEventListener('scroll', updateActiveSectionFromScroll)
    window.removeEventListener('resize', updateActiveSectionFromScroll)
    removeScrollSpy = null
  }
}

const selectSection = (section: SettingsSectionKey) => {
  activeSection.value = section
  if (!isDesktopViewport()) return
  document.getElementById(sectionID(section))?.scrollIntoView({
    behavior: 'smooth',
    block: 'start',
  })
}
</script>

<template>
  <div class="min-h-dvh bg-background">
    <div
      class="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 pb-28 pt-6 sm:px-6 md:pb-10 md:pt-8 lg:pt-10"
    >
      <header class="flex flex-col gap-5 border-b pb-5 md:pb-6">
        <BackButton to="/" :label="t('settings.back')" class="w-fit" />

        <div class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div class="min-w-0 space-y-2">
            <h1 class="text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
              {{ t('settings.title') }}
            </h1>
          </div>
          <SettingsSaveButton
            class="hidden md:inline-flex"
            :disabled="!isReady || isSaving"
            :saving="isSaving"
            @save="handleSave"
          />
        </div>
      </header>

      <div v-if="isLoading && !isReady" class="flex items-center justify-center py-24">
        <Spinner class="size-6 text-muted-foreground" />
        <span class="sr-only">{{ t('settings.loading') }}</span>
      </div>

      <div v-else-if="isReady" class="space-y-6">
        <Tabs
          v-model="activeSection"
          class="sticky top-0 z-20 -mx-4 border-b bg-background/95 px-4 py-3 backdrop-blur sm:-mx-6 sm:px-6 md:hidden"
        >
          <TabsList class="grid w-full grid-cols-3">
            <TabsTrigger v-for="section in sections" :key="section.key" :value="section.key">
              {{ section.label }}
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <div class="grid gap-8 md:grid-cols-[11rem_minmax(0,1fr)]">
          <aside class="hidden md:block">
            <nav class="sticky top-8 space-y-1" aria-label="Settings sections">
              <Button
                v-for="section in sections"
                :key="section.key"
                type="button"
                variant="ghost"
                class="flex h-auto w-full flex-col items-start justify-start rounded-md px-3 py-2 text-left text-sm whitespace-normal transition-colors hover:bg-muted"
                :class="
                  activeSection === section.key
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground'
                "
                @click="selectSection(section.key)"
              >
                <span class="font-medium">{{ section.label }}</span>
                <span class="mt-1 line-clamp-2 text-xs text-muted-foreground">
                  {{ section.description }}
                </span>
              </Button>
            </nav>
          </aside>

          <div class="space-y-8 md:space-y-10">
            <SettingsAuthSection
              :id="sectionID('auth')"
              :auth="authValues"
              :enabled-channels="enabledChannelSchemas"
              :fields="authFields"
              :disabled="isSaving"
              :class="activeSection === 'auth' ? 'block' : 'hidden md:block'"
              @update-field="setAuthValue"
            />

            <SettingsRootSection
              :id="sectionID('proxy')"
              section="proxy"
              :title="t('settings.proxyTitle')"
              :description="t('settings.proxyDescription')"
              :fields="proxyFields"
              :values="proxyValues"
              :disabled="isSaving"
              class="md:border-t md:pt-8"
              :class="activeSection === 'proxy' ? 'block' : 'hidden md:block'"
              @update-field="(key, value) => setRootValue('proxy', key, value)"
            />

            <SettingsChannelsSection
              :id="sectionID('channels')"
              :title="t('settings.channelsTitle')"
              :description="t('settings.channelsDescription')"
              :channels="channels"
              :disabled="isSaving"
              :expanded-channels="expandedChannels"
              :schemas="channelSchemas"
              :class="activeSection === 'channels' ? 'block' : 'hidden md:block'"
              @toggle-channel="toggleChannel"
              @toggle-details="toggleChannelDetails"
              @update-field="setChannelValue"
            />
          </div>
        </div>
      </div>

      <div
        class="fixed inset-x-0 bottom-0 z-30 border-t bg-background/95 p-4 shadow-[0_-12px_30px_rgba(15,23,42,0.08)] backdrop-blur md:hidden"
      >
        <SettingsSaveButton
          class="w-full"
          :disabled="!isReady || isSaving"
          :saving="isSaving"
          @save="handleSave"
        />
      </div>
    </div>
  </div>
</template>
