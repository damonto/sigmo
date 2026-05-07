<script setup lang="ts">
import { ArrowLeft } from 'lucide-vue-next'
import { computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { toast } from 'vue-sonner'

import ConfigAuthSection from '@/components/config/ConfigAuthSection.vue'
import ConfigChannelsSection from '@/components/config/ConfigChannelsSection.vue'
import ConfigRootSection from '@/components/config/ConfigRootSection.vue'
import ConfigSaveButton from '@/components/config/ConfigSaveButton.vue'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useConfig } from '@/composables/useConfig'
import { useConfigForm, type ConfigSectionKey } from '@/composables/useConfigForm'

const { t } = useI18n()
const { config, values, isLoading, isSaving, fetchConfig, saveConfig } = useConfig()
const {
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
} = useConfigForm(config, values)

const sections = computed(() => [
  { key: 'app' as const, label: t('config.appTitle'), description: t('config.appDescription') },
  {
    key: 'proxy' as const,
    label: t('config.proxyTitle'),
    description: t('config.proxyDescription'),
  },
  {
    key: 'channels' as const,
    label: t('config.channelsTitle'),
    description: t('config.channelsDescription'),
  },
])
const desktopMediaQuery = '(min-width: 768px)'
const scrollSpyOffset = 96
let isUnmounted = false
let removeScrollSpy: (() => void) | null = null

onMounted(async () => {
  await fetchConfig()
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
  const response = await saveConfig()
  if (!response) return

  initializeExpandedChannels()
  const restartFields = response.restartRequiredFields ?? []
  if (restartFields.length > 0) {
    toast.warning(t('config.restartRequired'))
    return
  }
  toast.success(t('config.saveSuccess'))
}

const sectionID = (section: ConfigSectionKey) => {
  return `config-section-${section}`
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

  let currentSection: ConfigSectionKey = 'app'
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

const selectSection = (section: ConfigSectionKey) => {
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
        <Button as-child variant="ghost" size="sm" class="w-fit px-0 text-muted-foreground">
          <RouterLink to="/">
            <ArrowLeft class="size-4" />
            {{ t('config.back') }}
          </RouterLink>
        </Button>

        <div class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div class="min-w-0 space-y-2">
            <h1 class="text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
              {{ t('config.title') }}
            </h1>
            <p class="break-all text-sm text-muted-foreground">
              {{ configPath }}
            </p>
            <p class="text-xs text-muted-foreground">
              {{ t('config.restartHint') }}
            </p>
          </div>
          <ConfigSaveButton
            class="hidden md:inline-flex"
            :disabled="!isReady || isSaving"
            :saving="isSaving"
            @save="handleSave"
          />
        </div>
      </header>

      <div v-if="isLoading && !isReady" class="flex items-center justify-center py-24">
        <Spinner class="size-6 text-muted-foreground" />
        <span class="sr-only">{{ t('config.loading') }}</span>
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
            <nav class="sticky top-8 space-y-1" aria-label="Configuration sections">
              <button
                v-for="section in sections"
                :key="section.key"
                type="button"
                class="flex w-full flex-col rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-muted"
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
              </button>
            </nav>
          </aside>

          <div class="space-y-8 md:space-y-10">
            <ConfigRootSection
              :id="sectionID('app')"
              section="app"
              :title="t('config.appTitle')"
              :description="t('config.appDescription')"
              :fields="appFields"
              :values="appValues"
              :disabled="isSaving"
              :class="activeSection === 'app' ? 'block' : 'hidden md:block'"
              @update-field="(key, value) => setRootValue('app', key, value)"
            />

            <ConfigAuthSection
              :app="appValues"
              :enabled-channels="enabledChannelSchemas"
              :fields="authFields"
              :disabled="isSaving"
              :class="activeSection === 'app' ? 'block' : 'hidden md:block'"
              @update-field="(key, value) => setRootValue('app', key, value)"
            />

            <ConfigRootSection
              :id="sectionID('proxy')"
              section="proxy"
              :title="t('config.proxyTitle')"
              :description="t('config.proxyDescription')"
              :fields="proxyFields"
              :values="proxyValues"
              :disabled="isSaving"
              class="md:border-t md:pt-8"
              :class="activeSection === 'proxy' ? 'block' : 'hidden md:block'"
              @update-field="(key, value) => setRootValue('proxy', key, value)"
            />

            <ConfigChannelsSection
              :id="sectionID('channels')"
              :title="t('config.channelsTitle')"
              :description="t('config.channelsDescription')"
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
        <ConfigSaveButton
          class="w-full"
          :disabled="!isReady || isSaving"
          :saving="isSaving"
          @save="handleSave"
        />
      </div>
    </div>
  </div>
</template>
