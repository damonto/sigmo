<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/select'
import type { Modem } from '@/types/modem'

const props = defineProps<{ modems: Modem[]; disabled: boolean; unavailable: boolean }>()
const selectedID = defineModel<string>({ required: true })
const { t } = useI18n()
const selected = computed(() => props.modems.find((modem) => modem.id === selectedID.value))
</script>
<template>
  <div class="space-y-2">
    <label
      v-if="modems.length > 1"
      class="grid min-w-0 gap-2 text-sm"
    >
      {{ t('communication.modem') }}
      <Select
        v-model="selectedID"
        :disabled="disabled"
      >
        <SelectTrigger
          class="min-h-12 w-full min-w-0 py-2 data-[size=default]:h-auto"
          :aria-label="t('communication.modem')"
        >
          <SelectValue :placeholder="t('communication.chooseModem')">
            <span
              v-if="selected"
              class="flex flex-col items-start text-left"
            >
              <span>{{ selected.name || selected.id }}</span>
              <span class="text-xs text-muted-foreground">{{
                selected.sim.label || selected.sim.operatorName
              }}</span>
            </span>
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="modem in modems"
            :key="modem.id"
            :value="modem.id"
          >
            <span class="flex flex-col items-start">
              <span>{{ modem.name || modem.id }}</span>
              <span class="text-xs text-muted-foreground">{{
                modem.sim.label || modem.sim.operatorName
              }}</span>
            </span>
          </SelectItem>
        </SelectContent>
      </Select>
    </label>
    <p
      v-else-if="modems.length === 1"
      class="text-sm text-muted-foreground"
    >
      {{ modems[0]?.sim.label || modems[0]?.sim.operatorName || modems[0]?.name || modems[0]?.id }}
    </p>
    <p
      v-if="unavailable && !selectedID"
      class="text-sm text-amber-600 dark:text-amber-400"
    >
      {{ t('communication.modemUnavailable') }}
    </p>
    <p
      v-if="!disabled && !modems.length"
      class="text-sm text-muted-foreground"
    >
      {{ t('communication.noModems') }}
    </p>
  </div>
</template>
