<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useHistoryAssignments } from '@/composables/useHistoryAssignments'

const props = defineProps<{ sourceId?: string }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const {
  source,
  target,
  busy,
  loading,
  error,
  pending,
  candidates,
  canAssign,
  assign,
  choose,
  close,
  retry,
} = useHistoryAssignments(
  computed(() => props.sourceId),
  () => emit('changed'),
)

const handleOpenChange = (open: boolean) => {
  if (!open) close()
}
</script>

<template>
  <div
    v-if="!props.sourceId && pending.length"
    class="rounded-2xl border bg-muted/40 p-4"
  >
    <p class="font-medium">{{ t('communication.deletedLines') }}</p>
    <Button
      v-for="line in pending"
      :key="line.profileId"
      variant="link"
      :disabled="busy"
      @click="choose(line)"
    >
      {{ line.number || line.label || line.profileId }} · {{ t('communication.assign') }}
    </Button>
  </div>
  <p
    v-if="error && !source"
    role="alert"
    class="text-sm text-destructive"
  >
    {{ error }}
  </p>
  <Dialog
    :open="!!source"
    @update:open="handleOpenChange"
  >
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{{ t('communication.assign') }}</DialogTitle>
        <DialogDescription>{{ t('communication.assignDescription') }}</DialogDescription>
      </DialogHeader>
      <p>{{ source?.number || source?.label || source?.profileId }}</p>
      <p class="text-sm text-muted-foreground">
        {{
          t('communication.historyCounts', {
            messages: source?.messageCount ?? 0,
            calls: source?.callCount ?? 0,
          })
        }}
      </p>
      <Select
        v-model="target"
        :disabled="busy || loading"
      >
        <SelectTrigger class="w-full">
          <SelectValue :placeholder="t('communication.chooseLine')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="line in candidates"
            :key="line.profileId"
            :value="line.profileId"
          >
            {{ line.number || line.label || line.profileId }} · {{ line.modemId }}
            {{ line.active ? '' : t('communication.inactive') }}
          </SelectItem>
        </SelectContent>
      </Select>
      <p
        v-if="loading || busy"
        role="status"
        class="text-sm text-muted-foreground"
      >
        {{ t('communication.loading') }}
      </p>
      <p v-else-if="!error && !candidates.length">{{ t('communication.noLines') }}</p>
      <div v-if="error">
        <p
          role="alert"
          class="text-sm text-destructive"
        >
          {{ error }}
        </p>
        <Button
          variant="outline"
          :disabled="busy || loading"
          @click="retry"
        >
          {{ t('communication.retry') }}
        </Button>
      </div>
      <div class="flex justify-end gap-2">
        <Button
          variant="outline"
          :disabled="busy"
          @click="close"
        >
          {{ t('communication.later') }}
        </Button>
        <Button
          :disabled="!canAssign"
          @click="assign"
        >
          {{ t('communication.assign') }}
        </Button>
      </div>
    </DialogContent>
  </Dialog>
</template>
