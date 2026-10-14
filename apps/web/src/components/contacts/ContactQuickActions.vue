<script setup lang="ts">
import { MessageSquare, Phone } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import CommunicationActionButton from '@/components/communication/CommunicationActionButton.vue'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import type { ContactNumber } from '@/types/communication'

defineProps<{
  numbers: Pick<ContactNumber, 'number' | 'label'>[]
  callDisabled?: boolean
  messageDisabled?: boolean
}>()
const emit = defineEmits<{ action: [number: string, mode: 'message' | 'call'] }>()
const { t } = useI18n()
const actions = [
  { mode: 'message', label: 'communication.message', icon: MessageSquare },
  { mode: 'call', label: 'communication.dial', icon: Phone },
] as const
</script>

<template>
  <div class="flex flex-wrap gap-2">
    <template
      v-for="shortcut in actions"
      :key="shortcut.mode"
    >
      <DropdownMenu v-if="numbers.length > 1">
        <DropdownMenuTrigger as-child>
          <CommunicationActionButton
            :disabled="shortcut.mode === 'call' ? callDisabled : messageDisabled"
            ><component
              :is="shortcut.icon"
              class="size-4"
            /><slot
              name="label"
              :label="t(shortcut.label)"
              >{{ t(shortcut.label) }}</slot
            ></CommunicationActionButton
          >
        </DropdownMenuTrigger>
        <DropdownMenuContent align="center">
          <DropdownMenuItem
            v-for="(phone, index) in numbers"
            :key="index"
            @select="emit('action', phone.number, shortcut.mode)"
          >
            <span class="flex flex-col gap-1"
              ><span>{{ phone.number }}</span
              ><span class="text-xs text-muted-foreground">{{ phone.label }}</span></span
            >
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CommunicationActionButton
        v-else
        :disabled="
          !numbers[0]?.number || (shortcut.mode === 'call' ? callDisabled : messageDisabled)
        "
        @click="emit('action', numbers[0]?.number || '', shortcut.mode)"
        ><component
          :is="shortcut.icon"
          class="size-4"
        /><slot
          name="label"
          :label="t(shortcut.label)"
          >{{ t(shortcut.label) }}</slot
        ></CommunicationActionButton
      >
    </template>
  </div>
</template>
