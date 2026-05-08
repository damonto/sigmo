<script setup lang="ts">
import { computed } from 'vue'
import { Check, ChevronDown } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useModemDisplay } from '@/composables/useModemDisplay'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  currentModem: Modem | null
  modems: Modem[]
}>()

const emit = defineEmits<{
  (event: 'select', modem: Modem): void
  (event: 'title-click'): void
}>()

const { t } = useI18n()
const { flagClass } = useModemDisplay()

const currentModemId = computed(() => props.currentModem?.id ?? '')
const canSwitchModems = computed(() => Boolean(props.currentModem) && props.modems.length > 1)

const displayModemName = (modem: Modem) => {
  return modem.name.trim() || modem.sim.operatorName || modem.id
}

const title = computed(() =>
  props.currentModem ? displayModemName(props.currentModem) : t('modemDetail.unknown'),
)

const handleSelect = (modem: Modem) => {
  if (modem.id === currentModemId.value) return
  emit('select', modem)
}
</script>

<template>
  <DropdownMenu v-if="canSwitchModems">
    <DropdownMenuTrigger as-child>
      <button
        type="button"
        class="group inline-flex max-w-full items-center gap-2 text-left text-3xl font-semibold tracking-tight text-foreground"
        :aria-label="t('modemDetail.switchModem')"
        :title="t('modemDetail.switchModem')"
        @click="emit('title-click')"
      >
        <span class="min-w-0 truncate">
          {{ title }}
        </span>
        <ChevronDown
          class="mt-1 size-5 shrink-0 text-muted-foreground transition group-data-[state=open]:rotate-180"
        />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" class="w-80 max-w-[calc(100vw-3rem)]">
      <DropdownMenuItem
        v-for="item in props.modems"
        :key="item.id"
        :class="['gap-2.5 px-2 py-2', item.id === currentModemId && 'bg-muted/60']"
        @click="handleSelect(item)"
      >
        <div
          class="flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30"
        >
          <span
            v-if="flagClass(item.sim.regionCode)"
            :class="flagClass(item.sim.regionCode)"
            class="rounded-sm text-base"
            :aria-label="item.sim.regionCode"
            :title="item.sim.regionCode"
          />
          <span v-else class="text-xs font-semibold text-muted-foreground">
            {{ item.sim.regionCode }}
          </span>
        </div>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-semibold leading-tight text-foreground">
            {{ displayModemName(item) }}
          </p>
          <p class="truncate text-xs leading-tight text-muted-foreground">
            {{ item.sim.operatorName }}
          </p>
        </div>
        <ModemSignalStatus
          :signal-quality="item.signalQuality"
          :registration-state="item.registrationState"
          size="sm"
        />
        <Check
          v-if="item.id === currentModemId"
          class="size-4 shrink-0 text-muted-foreground"
          :aria-label="t('modemDetail.currentModem')"
        />
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
  <h1
    v-else
    class="text-3xl font-semibold tracking-tight text-foreground"
    @click="emit('title-click')"
  >
    {{ title }}
  </h1>
</template>
