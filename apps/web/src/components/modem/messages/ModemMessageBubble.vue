<script setup lang="ts">
import MessageBubble from '@/components/communication/MessageBubble.vue'
import type { ThreadMessageItem } from '@/composables/useModemMessageThread'
defineProps<{ item: ThreadMessageItem }>()
</script>
<template><MessageBubble :item="item" /></template>
