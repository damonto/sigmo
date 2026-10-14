<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { useAvatarCrop } from '@/composables/useAvatarCrop'
const emit = defineEmits<{ selected: [image: Blob] }>()
const { t } = useI18n()
const { preview, zoom, horizontal, vertical, error, choose } = useAvatarCrop((image) =>
  emit('selected', image),
)
</script>
<template>
  <label class="grid gap-2"
    >{{ t('communication.avatar')
    }}<Input
      type="file"
      accept="image/*"
      @change="choose"
  /></label>
  <div
    v-if="preview"
    class="grid gap-3"
  >
    <img
      :src="preview"
      alt=""
      class="mx-auto size-32 rounded-full"
    /><label
      >{{ t('communication.zoom')
      }}<Input
        v-model="zoom"
        type="range"
        min="1"
        max="4"
        step="0.05" /></label
    ><label
      >{{ t('communication.horizontal')
      }}<Input
        v-model="horizontal"
        type="range"
        min="0"
        max="100" /></label
    ><label
      >{{ t('communication.vertical')
      }}<Input
        v-model="vertical"
        type="range"
        min="0"
        max="100"
    /></label>
  </div>
  <p
    v-if="error"
    role="alert"
    class="text-destructive"
  >
    {{ error }}
  </p>
</template>
