<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const alias = defineModel<string>('alias', { required: true })
const mss = defineModel<string>('mss', { required: true })
const compatible = defineModel<boolean>('compatible', { required: true })

const props = defineProps<{
  isLoading: boolean
  isUpdating: boolean
  isValid: boolean
}>()

const emit = defineEmits<{
  (event: 'update'): void
}>()

const { t } = useI18n()

const isInputDisabled = computed(() => props.isLoading || props.isUpdating)
const isActionDisabled = computed(() => !props.isValid || props.isUpdating)
</script>

<template>
  <section class="space-y-4 rounded-2xl bg-card p-4 shadow-sm">
    <div class="flex items-center justify-between gap-4">
      <h2 class="text-base font-semibold text-foreground">
        {{ t('modemDetail.settings.deviceTitle') }}
      </h2>
    </div>

    <div class="space-y-2">
      <Label for="modem-alias">{{ t('modemDetail.settings.aliasLabel') }}</Label>
      <Input
        id="modem-alias"
        v-model="alias"
        :disabled="isInputDisabled"
        :placeholder="t('modemDetail.settings.aliasPlaceholder')"
      />
      <p class="text-xs text-muted-foreground">
        {{ t('modemDetail.settings.aliasDescription') }}
      </p>
    </div>

    <div class="space-y-2">
      <Label for="modem-mss">{{ t('modemDetail.settings.mssLabel') }}</Label>
      <Input
        id="modem-mss"
        v-model="mss"
        type="number"
        min="64"
        max="254"
        step="1"
        :disabled="isInputDisabled"
        :placeholder="t('modemDetail.settings.mssPlaceholder')"
      />
      <p class="text-xs text-muted-foreground">
        {{ t('modemDetail.settings.mssDescription') }}
      </p>
    </div>

    <div class="space-y-2">
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="modem-compatible">{{ t('modemDetail.settings.compatibleLabel') }}</Label>
          <p class="text-xs leading-5 text-muted-foreground">
            {{ t('modemDetail.settings.compatibleDescription') }}
          </p>
        </div>
        <Switch
          id="modem-compatible"
          :model-value="compatible"
          :disabled="isInputDisabled"
          @update:model-value="(value: boolean) => (compatible = value)"
        />
      </div>
    </div>
    <div class="flex justify-end">
      <Button
        size="sm"
        type="button"
        class="w-full"
        :disabled="isActionDisabled"
        @click="emit('update')"
      >
        <span v-if="props.isUpdating" class="inline-flex items-center gap-2">
          <Spinner class="size-4" />
          {{ t('modemDetail.actions.update') }}
        </span>
        <span v-else>{{ t('modemDetail.actions.update') }}</span>
      </Button>
    </div>
  </section>
</template>
