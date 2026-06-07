<script setup lang="ts">
import { Delete, PhoneCall } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

export type ModemDialpadKey = {
  value: string
  letters: string
}

export type ModemDialpadDensity = 'regular' | 'compact'

const densityClasses = {
  regular: {
    root: 'gap-6',
    header: 'space-y-1',
    title: 'text-base',
    description: 'text-sm',
    inputWrap: 'min-h-24',
    input: 'h-20',
    backspaceButton: '',
    deleteIcon: 'size-5',
    keyGrid: 'max-w-60 gap-4',
    keyButton: 'text-lg',
    keyLetters: 'h-4 text-[0.65rem]',
    callIcon: 'size-5',
    spinner: 'size-6',
  },
  compact: {
    root: 'gap-4',
    header: 'space-y-0.5',
    title: 'text-sm',
    description: 'text-xs leading-snug',
    inputWrap: 'min-h-16',
    input: 'h-14',
    backspaceButton: 'size-8',
    deleteIcon: 'size-4',
    keyGrid: 'max-w-52 gap-3',
    keyButton: 'text-base',
    keyLetters: 'h-3 text-[0.6rem] leading-none',
    callIcon: 'size-4',
    spinner: 'size-5',
  },
} as const

const props = withDefaults(
  defineProps<{
    digits: string
    keys: ModemDialpadKey[]
    inputClass: string
    canDial: boolean
    isDialing: boolean
    showHeader?: boolean
    density?: ModemDialpadDensity
  }>(),
  {
    showHeader: false,
    density: 'regular',
  },
)

const emit = defineEmits<{
  (event: 'update:digits', value: string): void
  (event: 'backspace'): void
  (event: 'append-key', value: string): void
  (event: 'start-plus', value: string): void
  (event: 'clear-plus'): void
  (event: 'dial'): void
}>()

const { t } = useI18n()
const inputRef = ref<HTMLInputElement | null>(null)
const classes = computed(() => densityClasses[props.density])

const updateDigits = (event: Event) => {
  const target = event.target as HTMLInputElement | null
  emit('update:digits', target?.value ?? '')
}

defineExpose({
  focus: () => inputRef.value?.focus(),
})
</script>

<template>
  <div class="flex min-h-0 flex-col" :class="classes.root">
    <header v-if="props.showHeader" :class="classes.header">
      <h2 class="font-semibold text-foreground" :class="classes.title">
        {{ t('modemDetail.phone.dialpad') }}
      </h2>
      <p class="text-muted-foreground" :class="classes.description">
        {{ t('modemDetail.phone.dialpadDescription') }}
      </p>
    </header>

    <div class="relative flex items-center" :class="classes.inputWrap">
      <input
        ref="inputRef"
        :value="props.digits"
        type="tel"
        inputmode="tel"
        autocomplete="tel"
        class="w-full bg-transparent text-center font-semibold tracking-normal outline-none"
        :class="[classes.input, props.inputClass]"
        :aria-label="t('modemDetail.phone.numberPlaceholder')"
        @input="updateDigits"
        @keydown.enter.prevent="emit('dial')"
      />
      <Button
        v-if="props.digits"
        size="icon"
        variant="ghost"
        class="absolute top-1/2 right-0 -translate-y-1/2 touch-manipulation"
        :class="classes.backspaceButton"
        :aria-label="t('modemDetail.phone.backspace')"
        @click="emit('backspace')"
      >
        <Delete :class="classes.deleteIcon" />
      </Button>
    </div>

    <div class="mx-auto grid w-full grid-cols-3" :class="classes.keyGrid">
      <button
        v-for="key in props.keys"
        :key="key.value"
        type="button"
        class="flex aspect-square min-h-0 touch-manipulation select-none flex-col items-center justify-center rounded-full bg-muted font-semibold transition hover:bg-muted/70 active:scale-95"
        :class="classes.keyButton"
        @click="emit('append-key', key.value)"
        @pointerdown="emit('start-plus', key.value)"
        @pointerup="emit('clear-plus')"
        @pointercancel="emit('clear-plus')"
        @pointerleave="emit('clear-plus')"
      >
        <span>{{ key.value }}</span>
        <span class="font-medium text-muted-foreground" :class="classes.keyLetters">
          {{ key.letters }}
        </span>
      </button>
    </div>

    <div class="mx-auto grid w-full grid-cols-3" :class="classes.keyGrid">
      <button
        type="button"
        class="col-start-2 flex aspect-square min-h-0 touch-manipulation items-center justify-center rounded-full bg-emerald-600 text-white transition hover:bg-emerald-700 active:scale-95 disabled:pointer-events-none disabled:opacity-50"
        :disabled="!props.canDial"
        :aria-label="t('modemDetail.phone.call')"
        @click="emit('dial')"
      >
        <PhoneCall v-if="!props.isDialing" :class="classes.callIcon" />
        <Spinner v-else :class="classes.spinner" />
      </button>
    </div>
  </div>
</template>
