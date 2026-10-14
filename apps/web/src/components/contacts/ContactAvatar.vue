<script setup lang="ts">
import { computed, ref, watch, type HTMLAttributes } from 'vue'
import { communicationApi } from '@/apis/communication'
import { cn } from '@/lib/utils'
const props = defineProps<{
  id?: string
  name: string
  hasAvatar?: boolean
  updatedAt?: string
  color?: string
  class?: HTMLAttributes['class']
}>()
const url = ref('')
const initials = computed(() => props.name.trim().slice(0, 2).toLocaleUpperCase() || '?')
watch(
  () => [props.id, props.hasAvatar, props.updatedAt],
  async (_, __, cleanup) => {
    let cancelled = false
    let objectURL = ''
    url.value = ''
    cleanup(() => {
      cancelled = true
      if (objectURL) URL.revokeObjectURL(objectURL)
    })
    if (!props.id || !props.hasAvatar) return
    try {
      const blob = await communicationApi.avatar(props.id)
      if (cancelled) return
      objectURL = URL.createObjectURL(blob)
      url.value = objectURL
    } catch {
      /* Keep initials when an avatar cannot be fetched. */
    }
  },
  { immediate: true },
)
</script>
<template>
  <span
    :class="
      cn(
        'inline-flex size-11 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted text-base font-semibold',
        props.class,
      )
    "
    :style="{ backgroundColor: props.color || undefined }"
  >
    <img
      v-if="url"
      :src="url"
      alt=""
      class="size-full object-cover"
    />
    <span v-else>{{ initials }}</span>
  </span>
</template>
