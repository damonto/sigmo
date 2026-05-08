<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import ModemNetworkOption from '@/components/modem/settings/ModemNetworkOption.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { RadioGroup } from '@/components/ui/radio-group'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'
import type { NetworkResponse } from '@/types/network'

const open = defineModel<boolean>('open', { required: true })
const selectedNetwork = defineModel<string>('selectedNetwork', { required: true })

const props = defineProps<{
  networks: NetworkResponse[]
  isLoading: boolean
  isRegistering: boolean
  hasAvailableNetworks: boolean
  hasSelection: boolean
}>()

const emit = defineEmits<{
  (event: 'register'): void
}>()

const { t } = useI18n()
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.settings.networkDialogTitle') }}</DialogTitle>
        <DialogDescription>
          {{ t('modemDetail.settings.networkDialogDescription') }}
        </DialogDescription>
      </DialogHeader>

      <ScrollArea class="max-h-[60vh] pr-1">
        <div v-if="props.isLoading" class="flex items-center justify-center py-10">
          <Spinner class="size-6 text-muted-foreground" />
          <span class="sr-only">{{ t('modemDetail.actions.loading') }}</span>
        </div>

        <div v-else-if="props.hasAvailableNetworks" class="space-y-2">
          <RadioGroup v-model="selectedNetwork" class="gap-2">
            <ModemNetworkOption
              v-for="network in props.networks"
              :key="network.operatorCode"
              :network="network"
              :is-selected="selectedNetwork === network.operatorCode"
            />
          </RadioGroup>
        </div>

        <p v-else class="text-sm text-muted-foreground">
          {{ t('modemDetail.settings.networkEmpty') }}
        </p>
      </ScrollArea>

      <DialogFooter>
        <Button variant="ghost" type="button" :disabled="props.isRegistering" @click="open = false">
          {{ t('modemDetail.actions.cancel') }}
        </Button>
        <Button
          type="button"
          :disabled="!props.hasSelection || props.isRegistering"
          @click="emit('register')"
        >
          <span v-if="props.isRegistering" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.settings.networkRegister') }}
          </span>
          <span v-else>{{ t('modemDetail.settings.networkRegister') }}</span>
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
