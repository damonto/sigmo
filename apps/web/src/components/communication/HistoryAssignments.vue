<script setup lang="ts">
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
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ sourceId?: string }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const { source, target, busy, error, pending, candidates, canAssign, assign, choose } =
  useHistoryAssignments(
    computed(() => props.sourceId),
    () => emit('changed'),
  )
</script>
<template>
  <div
    v-if="pending.length"
    class="rounded-2xl border bg-muted/40 p-4"
  >
    <p class="font-medium">{{ t('communication.deletedLines') }}</p>
    <Button
      v-for="line in pending"
      :key="line.profileId"
      variant="link"
      @click="choose(line)"
      >{{ line.number || line.label || line.profileId }} · {{ t('communication.assign') }}</Button
    >
  </div>
  <p
    v-if="error"
    role="alert"
    class="text-sm text-destructive"
  >
    {{ error }}
  </p>
  <Dialog
    :open="!!source"
    @update:open="
      (open: boolean) => {
        if (!open && !busy) source = undefined
      }
    "
    ><DialogContent
      ><DialogHeader
        ><DialogTitle>{{ t('communication.assign') }}</DialogTitle
        ><DialogDescription>{{
          t('communication.assignDescription')
        }}</DialogDescription></DialogHeader
      >
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
        :disabled="busy"
      >
        <SelectTrigger class="w-full">
          <SelectValue :placeholder="t('communication.chooseLine')"
        /></SelectTrigger>
        <SelectContent
          ><SelectItem
            v-for="line in candidates"
            :key="line.profileId"
            :value="line.profileId"
            >{{ line.number || line.label || line.profileId }} · {{ line.modemId }}
            {{ line.active ? '' : t('communication.inactive') }}</SelectItem
          ></SelectContent
        ></Select
      >
      <p v-if="!candidates.length">{{ t('communication.noLines') }}</p>
      <div class="flex justify-end gap-2">
        <Button
          variant="outline"
          :disabled="busy"
          @click="source = undefined"
          >{{ t('communication.later') }}</Button
        ><Button
          :disabled="busy || !canAssign"
          @click="assign"
          >{{ t('communication.assign') }}</Button
        >
      </div>
    </DialogContent></Dialog
  >
</template>
