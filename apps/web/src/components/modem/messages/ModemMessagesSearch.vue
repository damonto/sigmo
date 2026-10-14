<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import CommunicationSearchInput from '@/components/communication/CommunicationSearchInput.vue'

const model = defineModel<string>({ required: true })
const { t } = useI18n()
</script>

<template>
  <CommunicationSearchInput
    v-model="model"
    :placeholder="t('modemDetail.messages.searchPlaceholder')"
    :clear-label="t('modemDetail.messages.clearSearch')"
  />
</template>
