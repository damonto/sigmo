<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'

import { Button, type ButtonVariants } from '@/components/ui/button'

const props = withDefaults(
  defineProps<{
    ariaLabel: string
    title?: string
    disabled?: boolean
    defaultBottom?: number
    minBottom?: number
    topPadding?: number
    dragThreshold?: number
    size?: ButtonVariants['size']
    variant?: ButtonVariants['variant']
  }>(),
  {
    disabled: false,
    defaultBottom: 96,
    minBottom: 24,
    topPadding: 24,
    dragThreshold: 4,
    size: 'icon-lg',
    variant: 'default',
  },
)

const emit = defineEmits<{
  (event: 'click'): void
}>()

const fabRef = useTemplateRef<HTMLElement>('fabRef')
const bottomOffset = ref(props.defaultBottom)
const isDragging = ref(false)
const hasDragged = ref(false)
const startY = ref(0)
const startBottom = ref(0)
const activePointerId = ref<number | null>(null)

const clampBottom = (value: number) => {
  const height = fabRef.value?.getBoundingClientRect().height ?? 0
  const maxBottom = Math.max(props.minBottom, window.innerHeight - height - props.topPadding)
  return Math.min(Math.max(value, props.minBottom), maxBottom)
}

const handlePointerDown = (event: PointerEvent) => {
  if (props.disabled) return
  const target = event.currentTarget as HTMLElement
  target.setPointerCapture(event.pointerId)
  activePointerId.value = event.pointerId
  isDragging.value = true
  hasDragged.value = false
  startY.value = event.clientY
  startBottom.value = bottomOffset.value
}

const handlePointerMove = (event: PointerEvent) => {
  if (!isDragging.value) return
  const delta = startY.value - event.clientY
  if (Math.abs(delta) > props.dragThreshold) {
    hasDragged.value = true
  }
  bottomOffset.value = clampBottom(startBottom.value + delta)
}

const handlePointerUp = (event: PointerEvent) => {
  if (!isDragging.value) return
  isDragging.value = false
  if (activePointerId.value !== null) {
    const target = event.currentTarget as HTMLElement
    target.releasePointerCapture(activePointerId.value)
    activePointerId.value = null
  }
}

const handleClick = () => {
  if (props.disabled) return
  if (hasDragged.value) {
    hasDragged.value = false
    return
  }
  emit('click')
}
</script>

<template>
  <div
    ref="fabRef"
    class="fixed right-6 z-20 inline-flex touch-none select-none"
    :style="{ bottom: `${bottomOffset}px` }"
    @pointerdown="handlePointerDown"
    @pointermove="handlePointerMove"
    @pointerup="handlePointerUp"
    @pointercancel="handlePointerUp"
    @click.self="handleClick"
  >
    <Button
      type="button"
      :variant="props.variant"
      :size="props.size"
      :aria-label="props.ariaLabel"
      :title="props.title ?? props.ariaLabel"
      :disabled="props.disabled"
      class="rounded-full shadow-xl hover:-translate-y-0.5"
      :class="isDragging ? 'transition-none' : 'transition'"
      @click="handleClick"
    >
      <slot />
    </Button>
  </div>
</template>
