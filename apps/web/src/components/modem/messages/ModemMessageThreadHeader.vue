<script setup lang="ts">
import { Ellipsis, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import BackButton from '@/components/BackButton.vue'
import CommunicationActionButton from '@/components/communication/CommunicationActionButton.vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const props = defineProps<{
  title: string
  canDelete: boolean
}>()

const emit = defineEmits<{
  (event: 'back'): void
  (event: 'delete'): void
}>()

// Consumers declare each action once; the header supplies its control and activation event.
defineSlots<{
  actions?(props: {
    actionComponent: typeof CommunicationActionButton | typeof DropdownMenuItem
    activateEvent: 'click' | 'select'
  }): unknown
}>()

const { t } = useI18n()
</script>

<template>
  <div
    class="grid grid-cols-[5rem_minmax(0,1fr)_5rem] items-center gap-3 border-b border-border pb-3 lg:flex"
  >
    <BackButton
      class="justify-self-start lg:hidden"
      :label="t('modemDetail.back')"
      @click="emit('back')"
    />
    <h2
      class="min-w-0 flex-1 truncate text-center text-lg font-semibold text-foreground lg:text-left"
    >
      {{ props.title }}
    </h2>
    <div class="ml-auto hidden items-center justify-end gap-2 lg:flex">
      <slot
        name="actions"
        :action-component="CommunicationActionButton"
        activate-event="click"
      />
      <CommunicationActionButton
        destructive
        type="button"
        class="justify-self-end"
        :aria-label="t('modemDetail.actions.delete')"
        :disabled="!props.canDelete"
        @click="emit('delete')"
      >
        <Trash2 class="size-4" />
        {{ t('modemDetail.actions.delete') }}
      </CommunicationActionButton>
    </div>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button
          variant="outline"
          size="icon"
          class="shrink-0 justify-self-end lg:hidden"
          :aria-label="t('communication.actions')"
        >
          <Ellipsis class="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        class="lg:hidden"
      >
        <slot
          name="actions"
          :action-component="DropdownMenuItem"
          activate-event="select"
        />
        <DropdownMenuItem
          variant="destructive"
          :disabled="!props.canDelete"
          @select="emit('delete')"
        >
          <Trash2 class="size-4" />{{ t('modemDetail.actions.delete') }}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>
</template>
