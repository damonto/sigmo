<script setup lang="ts">
import { computed } from 'vue'
import { Search, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const props = defineProps<{
  modelValue: string
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
}>()

const { t } = useI18n()

const searchValue = computed({
  get: () => props.modelValue,
  set: (value: string | number) => emit('update:modelValue', String(value)),
})
const hasSearchValue = computed(() => props.modelValue.trim().length > 0)

const clearSearch = () => {
  emit('update:modelValue', '')
}
</script>

<template>
  <div class="relative">
    <Search
      class="pointer-events-none absolute left-3.5 top-1/2 z-10 size-4 -translate-y-1/2 text-muted-foreground"
    />
    <Input
      v-model="searchValue"
      type="search"
      :placeholder="t('modemDetail.messages.searchPlaceholder')"
      :aria-label="t('modemDetail.messages.searchPlaceholder')"
      class="h-11 rounded-xl border-white/70 bg-card/85 pl-11 pr-10 shadow-sm backdrop-blur-xl dark:border-white/10 dark:bg-card/70"
    />
    <Button
      v-if="hasSearchValue"
      type="button"
      variant="ghost"
      size="icon-sm"
      class="absolute right-1.5 top-1/2 -translate-y-1/2 rounded-full text-muted-foreground hover:text-foreground"
      :aria-label="t('modemDetail.messages.clearSearch')"
      @click="clearSearch"
    >
      <X class="size-4" />
    </Button>
  </div>
</template>
