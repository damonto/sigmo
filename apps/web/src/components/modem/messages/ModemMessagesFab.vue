<script setup lang="ts">
import { Plus } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import DraggableFab from '@/components/fab/DraggableFab.vue'

const props = defineProps<{
  disabled: boolean
}>()

const emit = defineEmits<{
  (event: 'click'): void
}>()

const { t } = useI18n()
</script>

<template>
  <DraggableFab
    :ariaLabel="t('modemDetail.messages.newConversation')"
    :title="t('modemDetail.messages.newConversation')"
    :disabled="props.disabled"
    @click="emit('click')"
  >
    <Plus class="size-5" />
  </DraggableFab>
</template>
