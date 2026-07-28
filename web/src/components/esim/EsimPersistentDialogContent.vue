<script setup lang="ts">
import type { DialogContentEmits, DialogContentProps } from 'reka-ui'
import type { HTMLAttributes } from 'vue'
import { reactiveOmit } from '@vueuse/core'
import { useForwardPropsEmits } from 'reka-ui'

import { DialogContent } from '@/components/ui/dialog'

const props = withDefaults(
  defineProps<DialogContentProps & {
    class?: HTMLAttributes['class']
    showCloseButton?: boolean
  }>(),
  {
    showCloseButton: true,
  },
)

const emits = defineEmits<DialogContentEmits>()

defineOptions({
  inheritAttrs: false,
})

const delegatedProps = reactiveOmit(props, 'class', 'showCloseButton')
const forwarded = useForwardPropsEmits(delegatedProps, emits)

const preventDialogDismiss = (event: Event) => {
  event.preventDefault()
}
</script>

<template>
  <DialogContent
    v-bind="{ ...$attrs, ...forwarded }"
    :class="props.class"
    :show-close-button="props.showCloseButton"
    @interact-outside="preventDialogDismiss"
    @escape-key-down="preventDialogDismiss"
  >
    <slot />
  </DialogContent>
</template>
