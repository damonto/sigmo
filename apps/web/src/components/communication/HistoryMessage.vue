<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import MessageBubble from '@/components/communication/MessageBubble.vue'
import type { ThreadMessage } from '@/types/communication'
const props = defineProps<{ message: ThreadMessage; source: string; deleted?: boolean }>()
const emit = defineEmits<{ visible: [id: number] }>()
const { t } = useI18n()
const item = computed(() => ({
  text: props.message.text,
  incoming: props.message.incoming,
  timestampLabel: new Date(props.message.timestamp).toLocaleString(),
  status: props.message.status,
}))
const root = useTemplateRef<HTMLElement>('root')
let observer: IntersectionObserver | undefined
let intersecting = false
const acknowledge = () => {
  if (intersecting && document.visibilityState === 'visible') emit('visible', props.message.id)
}
onMounted(() => {
  observer = new IntersectionObserver(
    ([entry]) => {
      intersecting = entry?.isIntersecting ?? false
      acknowledge()
    },
    { threshold: 0.5 },
  )
  if (root.value) observer.observe(root.value)
  document.addEventListener('visibilitychange', acknowledge)
})
onBeforeUnmount(() => {
  observer?.disconnect()
  document.removeEventListener('visibilitychange', acknowledge)
})
</script>
<template>
  <article ref="root">
    <MessageBubble :item="item">
      <template #source>
        <p>
          {{ source }}
          <span v-if="deleted"> · {{ t('communication.deletedNumber') }}</span>
        </p>
        <p v-if="message.historyProfileId !== message.profileId">
          {{ t('communication.originalLine') }}
        </p>
      </template>
    </MessageBubble>
  </article>
</template>
