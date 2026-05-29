<script setup lang="ts">
import { computed } from 'vue'
import { Pencil, Smartphone, Wifi } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useModemDisplay } from '@/composables/useModemDisplay'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  modem: Modem | null
  operatorLabel: string
  accessTechnology: string
}>()

const emit = defineEmits<{
  (event: 'edit'): void
}>()

const { t } = useI18n()
const { signalIcon } = useModemDisplay()

const phoneCountry = computed(() => props.modem?.sim?.regionCode ?? '')
const rawMsisdn = computed(() => props.modem?.number?.trim() ?? '')
const displayMsisdn = computed(() => {
  if (!rawMsisdn.value) return t('modemDetail.settings.lineNoNumber')
  return formatPhoneDisplay(rawMsisdn.value, phoneCountry.value)
})
const accessTechnologyLabel = computed(() => {
  const value = props.accessTechnology.trim()
  return value || t('modemDetail.settings.networkUnknown')
})
const signalIconComponent = computed(() => signalIcon(props.modem?.signalQuality ?? 0))
</script>

<template>
  <Card
    class="overflow-hidden rounded-2xl border-primary/15 bg-primary/5 py-0 shadow-sm dark:bg-primary/10"
  >
    <CardContent class="p-0">
      <div class="flex items-center gap-3 border-b border-primary/10 p-3.5">
        <div
          class="flex size-10 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary"
        >
          <Smartphone class="size-4" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="min-h-6 truncate text-lg font-medium text-foreground">
            {{ displayMsisdn }}
          </p>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          class="rounded-full"
          :aria-label="t('modemDetail.settings.msisdnEdit')"
          @click="emit('edit')"
        >
          <Pencil class="size-4" />
        </Button>
      </div>

      <div class="flex items-center gap-3 px-3.5 py-2.5 text-xs">
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium text-foreground" :title="props.operatorLabel">
            {{ props.operatorLabel }}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-3 text-primary">
          <component
            :is="signalIconComponent"
            class="size-4 shrink-0 text-primary"
            :aria-label="t('labels.signal')"
            data-testid="line-signal-icon"
          />
          <span
            class="font-mono text-[11px] font-semibold text-foreground"
            :aria-label="t('modemDetail.settings.networkAccess')"
            :title="accessTechnologyLabel"
          >
            {{ accessTechnologyLabel }}
          </span>
          <Wifi
            v-if="props.modem?.wifiCallingConnected"
            class="size-4 shrink-0 text-primary"
            title="Wi-Fi Calling"
            aria-label="Wi-Fi Calling"
          />
        </div>
      </div>
    </CardContent>
  </Card>
</template>
