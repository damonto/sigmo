<script setup lang="ts">
import { ChevronLeft } from 'lucide-vue-next'
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { Button } from '@/components/ui/button'
import DesktopTabNav from '@/components/DesktopTabNav.vue'
import ModemTitleSwitcher from '@/components/modem/ModemTitleSwitcher.vue'
import { useModems } from '@/composables/useModems'
import type { NavigationItem } from '@/types/navigation'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  items: NavigationItem[]
  modemId: string
}>()

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { modems, fetchModems } = useModems()

const currentModem = computed(
  () => modems.value.find((modem) => modem.id === props.modemId) ?? null,
)

const switchRouteName = computed(() => {
  if (route.name === 'modem-message-thread') return 'modem-messages'
  if (route.name === 'modem-contact') return 'modem-contacts'
  if (typeof route.name === 'string' && route.name.startsWith('modem-')) return route.name
  return 'modem-detail'
})

const handleModemSwitch = (modem: Modem) => {
  if (modem.id === props.modemId) return
  void router.push({ name: switchRouteName.value, params: { id: modem.id } })
}

onMounted(() => {
  if (modems.value.length > 0) return
  void fetchModems()
})
</script>

<template>
  <header
    class="sticky top-0 z-20 hidden border-b border-border bg-background/85 backdrop-blur-xl backdrop-saturate-125 lg:block"
  >
    <div class="mx-auto flex h-16 w-full max-w-7xl items-center gap-5 px-8">
      <Button
        as-child
        variant="ghost"
        size="sm"
        class="gap-2 px-2"
      >
        <RouterLink :to="{ name: 'home' }">
          <ChevronLeft class="size-4" />
          {{ t('modemDetail.back') }}
        </RouterLink>
      </Button>

      <div class="min-w-0 border-l border-border pl-5">
        <ModemTitleSwitcher
          :current-modem="currentModem"
          :modems="modems"
          variant="compact"
          @select="handleModemSwitch"
        />
      </div>

      <DesktopTabNav :items="props.items" />
    </div>
  </header>
</template>
