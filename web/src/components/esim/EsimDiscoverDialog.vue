<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'
import type { EsimDiscoverItem } from '@/types/esim'

const open = defineModel<boolean>('open', { required: true })
const selectedAddress = defineModel<string>('selectedAddress', { required: true })

const props = defineProps<{
  options: EsimDiscoverItem[]
  isLoading: boolean
  hasOptions: boolean
  hasSelection: boolean
}>()

const emit = defineEmits<{
  (event: 'confirm'): void
}>()

const { t } = useI18n()
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.esim.discoverTitle') }}</DialogTitle>
        <DialogDescription>
          {{ t('modemDetail.esim.discoverDescription') }}
        </DialogDescription>
      </DialogHeader>

      <ScrollArea class="max-h-[60vh] pr-1">
        <div v-if="props.isLoading" class="flex items-center justify-center py-10">
          <Spinner class="size-6 text-muted-foreground" />
          <span class="sr-only">{{ t('modemDetail.actions.loading') }}</span>
        </div>

        <div v-else-if="props.hasOptions" class="space-y-2">
          <RadioGroup v-model="selectedAddress" class="gap-2">
            <label
              v-for="option in props.options"
              :key="option.eventId"
              class="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2 shadow-sm transition"
              :class="
                selectedAddress === option.address
                  ? 'border-primary/40 bg-primary/5'
                  : 'border-transparent bg-muted/30'
              "
            >
              <RadioGroupItem
                :id="`discover-${option.eventId}`"
                :value="option.address"
                class="mt-1"
              />
              <div class="min-w-0 space-y-1">
                <p class="text-sm font-semibold text-foreground">{{ option.address }}</p>
                <p class="text-xs text-muted-foreground">{{ option.eventId }}</p>
              </div>
            </label>
          </RadioGroup>
        </div>

        <p v-else class="text-sm text-center text-muted-foreground">
          {{ t('modemDetail.esim.discoverEmpty') }}
        </p>
      </ScrollArea>

      <DialogFooter>
        <Button variant="ghost" type="button" @click="open = false">
          {{ t('modemDetail.actions.cancel') }}
        </Button>
        <Button
          type="button"
          :disabled="!props.hasSelection || props.isLoading"
          @click="emit('confirm')"
        >
          {{ t('modemDetail.esim.discoverConfirm') }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
