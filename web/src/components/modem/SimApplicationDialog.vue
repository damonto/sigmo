<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronLeft } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import type { SimApplicationMenuItem, SimApplicationView } from '@/types/simApplication'

const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{
  view: SimApplicationView | null
}>()

const emit = defineEmits<{
  (event: 'select-menu-item', item: SimApplicationMenuItem): void
  (event: 'submit-input', text: string): void
  (event: 'submit-inkey', text: string): void
  (event: 'respond-confirm', accepted: boolean): void
  (event: 'back'): void
}>()

const { t } = useI18n()
const text = ref('')
const wrappedTextClass =
  'min-w-0 max-w-full whitespace-pre-wrap break-words text-sm leading-6 text-foreground [overflow-wrap:anywhere]'

const title = computed(() => {
  const view = props.view
  if (!view) return t('modemDetail.simApplication.title')
  if (view.type === 'menu') return view.menu.title || t('modemDetail.simApplication.title')
  if (view.type === 'display_text') return t('modemDetail.simApplication.messageTitle')
  if (view.type === 'input' || view.type === 'inkey')
    return t('modemDetail.simApplication.inputTitle')
  return t('modemDetail.simApplication.confirmTitle')
})

const isRootMenu = computed(() => props.view?.type === 'menu' && props.view.menu.kind === 'root')
const menuItems = computed(() => (props.view?.type === 'menu' ? props.view.menu.items : []))
const inputMaxLength = computed(() => {
  if (props.view?.type !== 'input') return undefined
  return props.view.maxLength > 0 ? props.view.maxLength : undefined
})

const handleOpenChange = (nextOpen: boolean) => {
  if (nextOpen) {
    open.value = true
    return
  }
  dismiss()
}

const dismiss = () => {
  const view = props.view
  if (!view || isRootMenu.value) {
    open.value = false
    return
  }
  if (view.type === 'menu' || view.type === 'input' || view.type === 'inkey') {
    emit('back')
    open.value = false
    return
  }
  emit('respond-confirm', false)
  open.value = false
}

const submitInput = () => {
  emit('submit-input', text.value)
}

const submitInkey = () => {
  emit('submit-inkey', text.value)
}

watch(
  () => props.view,
  (view) => {
    if (view?.type === 'input') {
      text.value = view.defaultText ?? ''
      return
    }
    text.value = ''
  },
  { immediate: true },
)
</script>

<template>
  <Dialog :open="open" @update:open="handleOpenChange">
    <DialogContent class="sm:max-w-md">
      <DialogHeader class="min-w-0">
        <div v-if="props.view?.type === 'menu' && !isRootMenu" class="flex items-center gap-2">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            class="-ml-2 size-8 shrink-0"
            :aria-label="t('modemDetail.actions.cancel')"
            @click="emit('back')"
          >
            <ChevronLeft class="size-4" />
          </Button>
          <DialogTitle class="min-w-0 truncate">{{ title }}</DialogTitle>
        </div>
        <DialogTitle v-else class="min-w-0 break-words [overflow-wrap:anywhere]">
          {{ title }}
        </DialogTitle>
        <DialogDescription class="sr-only">
          {{ title }}
        </DialogDescription>
      </DialogHeader>

      <div
        v-if="props.view?.type === 'menu'"
        class="overflow-hidden rounded-lg border bg-card shadow-sm"
      >
        <ScrollArea class="**:data-[slot=scroll-area-viewport]:max-h-[60vh]">
          <div class="divide-y">
            <button
              v-for="item in menuItems"
              :key="item.id"
              type="button"
              class="flex min-h-10 w-full items-center px-3 py-2 text-left text-sm font-medium leading-5 text-foreground transition hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              @click="emit('select-menu-item', item)"
            >
              <span class="min-w-0 flex-1 whitespace-normal break-words [overflow-wrap:anywhere]">
                {{ item.label }}
              </span>
            </button>
          </div>
        </ScrollArea>
      </div>

      <div v-else-if="props.view?.type === 'display_text'" class="space-y-4">
        <p :class="wrappedTextClass">
          {{ props.view.text }}
        </p>
        <DialogFooter>
          <Button type="button" class="w-full" @click="emit('respond-confirm', true)">
            {{ t('modemDetail.actions.confirm') }}
          </Button>
        </DialogFooter>
      </div>

      <form
        v-else-if="props.view?.type === 'input'"
        class="space-y-4"
        @submit.prevent="submitInput"
      >
        <p :class="wrappedTextClass">
          {{ props.view.text }}
        </p>
        <Input
          v-model="text"
          :type="props.view.hideInput ? 'password' : 'text'"
          :minlength="props.view.minLength"
          :maxlength="inputMaxLength"
          autofocus
        />
        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button type="button" variant="ghost" class="order-2 w-full sm:order-1" @click="dismiss">
            {{ t('modemDetail.actions.cancel') }}
          </Button>
          <Button type="submit" class="order-1 w-full sm:order-2">
            {{ t('modemDetail.actions.confirm') }}
          </Button>
        </DialogFooter>
      </form>

      <form
        v-else-if="props.view?.type === 'inkey'"
        class="space-y-4"
        @submit.prevent="submitInkey"
      >
        <p :class="wrappedTextClass">
          {{ props.view.text }}
        </p>
        <div v-if="props.view.yesNo" class="grid grid-cols-2 gap-3">
          <Button type="button" variant="outline" @click="emit('submit-inkey', 'N')">
            {{ t('modemDetail.simApplication.no') }}
          </Button>
          <Button type="button" @click="emit('submit-inkey', 'Y')">
            {{ t('modemDetail.simApplication.yes') }}
          </Button>
        </div>
        <template v-else>
          <Input v-model="text" maxlength="1" autofocus />
          <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Button
              type="button"
              variant="ghost"
              class="order-2 w-full sm:order-1"
              @click="dismiss"
            >
              {{ t('modemDetail.actions.cancel') }}
            </Button>
            <Button type="submit" class="order-1 w-full sm:order-2">
              {{ t('modemDetail.actions.confirm') }}
            </Button>
          </DialogFooter>
        </template>
      </form>

      <div v-else-if="props.view?.type === 'confirm'" class="space-y-4">
        <p :class="wrappedTextClass">
          {{ props.view.text || props.view.command }}
        </p>
        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button type="button" variant="ghost" class="order-2 w-full sm:order-1" @click="dismiss">
            {{ t('modemDetail.actions.cancel') }}
          </Button>
          <Button
            type="button"
            class="order-1 w-full sm:order-2"
            @click="emit('respond-confirm', true)"
          >
            {{ t('modemDetail.actions.confirm') }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>
