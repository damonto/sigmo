<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { EllipsisVertical } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useForm } from 'vee-validate'
import { toTypedSchema } from '@vee-validate/zod'
import * as z from 'zod'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useEsimApi } from '@/apis/esim'
import { useModemDisplay } from '@/composables/useModemDisplay'
import type { EsimProfile } from '@/types/esim'

const profiles = defineModel<EsimProfile[]>('profiles', { required: true })
const props = withDefaults(
  defineProps<{
    modemId: string
    loading?: boolean
    refreshModem?: () => Promise<void>
  }>(),
  {
    loading: false,
  },
)
const emit = defineEmits<{
  (event: 'success', message: string): void
}>()

const { t } = useI18n()
const esimApi = useEsimApi()
const { flagClass } = useModemDisplay()

const profileCount = computed(() => profiles.value.length)
const hasProfiles = computed(() => profiles.value.length > 0)
const isLoading = computed(() => props.loading)

const toggleOpen = ref(false)
const toggleProfile = ref<EsimProfile | null>(null)
const toggleNextValue = ref(false)
const toggleLoading = ref(false)

const renameOpen = ref(false)
const renameProfile = ref<EsimProfile | null>(null)

const deleteOpen = ref(false)
const deleteProfile = ref<EsimProfile | null>(null)
const deleteLoading = ref(false)

const isWithinMaxBytes = (value: string, maxBytes: number) =>
  new TextEncoder().encode(value).length <= maxBytes

const renameSchemaDefinition = z.object({
  name: z
    .string()
    .trim()
    .min(1, t('modemDetail.validation.required'))
    .refine((value) => isWithinMaxBytes(value, 64), t('modemDetail.validation.maxBytes')),
})

type RenameFormValues = z.infer<typeof renameSchemaDefinition>

const renameSchema = toTypedSchema(renameSchemaDefinition)

const { handleSubmit: handleRenameSubmit, resetForm: resetRenameForm, isSubmitting: renameSubmitting } =
  useForm<RenameFormValues>({
    validationSchema: renameSchema,
    initialValues: {
      name: '',
    },
  })

const openToggleDialog = (profile: EsimProfile, nextValue: boolean) => {
  toggleOpen.value = true
  toggleProfile.value = profile
  toggleNextValue.value = nextValue
}

const handleToggle = (profile: EsimProfile, nextValue: boolean) => {
  if (!nextValue) return
  openToggleDialog(profile, nextValue)
}

const closeToggleDialog = () => {
  toggleOpen.value = false
  toggleProfile.value = null
  toggleNextValue.value = false
  toggleLoading.value = false
}

const confirmToggle = async () => {
  if (!toggleProfile.value) return
  if (!toggleNextValue.value) {
    closeToggleDialog()
    return
  }
  toggleLoading.value = true
  try {
    const profileName =
      toggleProfile.value?.name ?? t('modemDetail.esim.downloadCompletedFallbackName')
    await esimApi.enableEsim(props.modemId, toggleProfile.value.iccid)
    if (!props.refreshModem) {
      toggleProfile.value.enabled = true
    }
    closeToggleDialog()
    if (props.refreshModem) {
      await props.refreshModem()
    }
    emit('success', t('modemDetail.esim.enableSuccess', { name: profileName }))
  } catch (err) {
    console.error('[EsimProfileSection] Failed to enable profile:', err)
  } finally {
    toggleLoading.value = false
  }
}

const openRenameDialog = (profile: EsimProfile) => {
  renameOpen.value = true
  renameProfile.value = profile
  resetRenameForm({ values: { name: profile.name } })
}

const closeRenameDialog = () => {
  renameOpen.value = false
  renameProfile.value = null
  resetRenameForm({ values: { name: '' } })
}

const confirmRename = handleRenameSubmit(async (values) => {
  if (!renameProfile.value) return
  try {
    await esimApi.updateEsimNickname(
      props.modemId,
      renameProfile.value.iccid,
      values.name,
    )
    renameProfile.value.name = values.name
    closeRenameDialog()
  } catch (err) {
    console.error('[EsimProfileSection] Failed to update nickname:', err)
  }
})

const openDeleteDialog = (profile: EsimProfile) => {
  if (profile.enabled) return
  deleteOpen.value = true
  deleteProfile.value = profile
}

const closeDeleteDialog = () => {
  deleteOpen.value = false
  deleteProfile.value = null
  deleteLoading.value = false
}

const confirmDelete = async (event?: Event) => {
  event?.preventDefault()
  if (!deleteProfile.value) return
  deleteLoading.value = true
  try {
    await esimApi.deleteEsim(props.modemId, deleteProfile.value.iccid)
    profiles.value = profiles.value.filter((profile) => profile.id !== deleteProfile.value?.id)
    closeDeleteDialog()
  } catch (err) {
    console.error('[EsimProfileSection] Failed to delete profile:', err)
  } finally {
    deleteLoading.value = false
  }
}

const handleRenameClick = (profile: EsimProfile) => {
  openRenameDialog(profile)
}

const handleDeleteClick = (profile: EsimProfile) => {
  openDeleteDialog(profile)
}

const togglePrompt = computed(() => {
  const name = toggleProfile.value?.name ?? ''
  return toggleNextValue.value
    ? t('modemDetail.confirm.enable', { name })
    : t('modemDetail.confirm.disable', { name })
})

const deletePrompt = computed(() =>
  t('modemDetail.confirm.delete', { name: deleteProfile.value?.name ?? '' }),
)

watch(renameOpen, (value) => {
  if (value) return
  renameProfile.value = null
  resetRenameForm({ values: { name: '' } })
})
</script>

<template>
  <section class="space-y-3">
    <div class="flex items-center justify-between">
      <h2 class="text-sm font-semibold text-muted-foreground">
        {{ t('modemDetail.esim.listTitle') }}
      </h2>
      <Badge variant="outline" class="text-[10px] uppercase tracking-[0.2em]">
        {{ isLoading ? '...' : profileCount }}
      </Badge>
    </div>

    <div v-if="isLoading" class="space-y-3">
      <div
        v-for="i in 3"
        :key="`esim-profile-skeleton-${i}`"
        class="flex items-center justify-between rounded-lg bg-card px-4 py-3 shadow-sm"
      >
        <div class="flex min-w-0 items-center gap-3">
          <Skeleton class="h-11 w-11 shrink-0 rounded-md bg-muted/80" />
          <div class="flex min-w-0 flex-col gap-1.5">
            <Skeleton class="h-4 w-28 rounded bg-muted/60" />
            <Skeleton class="h-3.5 w-40 rounded bg-muted/40" />
          </div>
        </div>
        <Skeleton class="h-6 w-11 rounded-full bg-muted/60" />
      </div>
    </div>

    <div
      v-else-if="!hasProfiles"
      class="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground"
    >
      {{ t('modemDetail.esim.noProfiles') }}
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="profile in profiles"
        :key="profile.id"
        class="flex items-center justify-between rounded-lg bg-card px-4 py-3 shadow-sm"
      >
        <div class="flex min-w-0 items-center gap-3">
          <div
            class="flex size-11 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30"
          >
            <img
              v-if="profile.logoUrl"
              :src="profile.logoUrl"
              :alt="`${profile.name} logo`"
              class="size-6 object-contain"
            />
            <span v-else class="rounded-sm text-[18px]">
              <span v-if="flagClass(profile.regionCode)" :class="flagClass(profile.regionCode)" />
              <span v-else class="text-xs font-semibold text-muted-foreground">
                {{ profile.regionCode }}
              </span>
            </span>
          </div>
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-foreground">
              {{ profile.name }}
            </p>
            <p class="truncate text-xs text-muted-foreground">
              {{ profile.iccid }}
            </p>
          </div>
        </div>

        <div class="flex items-center gap-3">
          <Switch
            :model-value="profile.enabled"
            :disabled="profile.enabled"
            @update:model-value="(nextValue) => handleToggle(profile, nextValue)"
          />

          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button variant="ghost" size="icon" type="button" aria-label="Profile actions">
                <EllipsisVertical class="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="w-40">
              <DropdownMenuItem @click="handleRenameClick(profile)">
                {{ t('modemDetail.actions.rename') }}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                :disabled="profile.enabled"
                :class="profile.enabled ? 'text-muted-foreground' : 'text-destructive'"
                @click="handleDeleteClick(profile)"
              >
                {{ t('modemDetail.actions.delete') }}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </div>
  </section>

  <AlertDialog v-model:open="toggleOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ togglePrompt }}</AlertDialogTitle>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel @click="closeToggleDialog" :disabled="toggleLoading">
          {{ t('modemDetail.actions.cancel') }}
        </AlertDialogCancel>
        <Button type="button" @click="confirmToggle" :disabled="toggleLoading">
          <span v-if="toggleLoading" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.actions.confirm') }}
          </span>
          <span v-else>{{ t('modemDetail.actions.confirm') }}</span>
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>

  <Dialog v-model:open="renameOpen">
    <DialogContent class="sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.actions.rename') }}</DialogTitle>
      </DialogHeader>
      <form class="space-y-4" @submit="confirmRename">
        <FormField v-slot="{ componentField }" name="name" :validateOnBlur="false">
          <FormItem>
            <FormLabel>{{ t('modemDetail.esim.nickname') }}</FormLabel>
            <FormControl>
              <Input
                type="text"
                :placeholder="t('modemDetail.esim.nickname')"
                v-bind="componentField"
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button type="submit" class="order-1 w-full sm:order-2" :disabled="renameSubmitting">
            <span v-if="renameSubmitting" class="inline-flex items-center gap-2">
              <Spinner class="size-4" />
              {{ t('modemDetail.actions.update') }}
            </span>
            <span v-else>{{ t('modemDetail.actions.update') }}</span>
          </Button>
          <Button
            variant="ghost"
            type="button"
            class="order-2 w-full sm:order-1"
            @click="closeRenameDialog"
            :disabled="renameSubmitting"
          >
            {{ t('modemDetail.actions.cancel') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>

  <AlertDialog v-model:open="deleteOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ deletePrompt }}</AlertDialogTitle>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel @click="closeDeleteDialog" :disabled="deleteLoading">
          {{ t('modemDetail.actions.cancel') }}
        </AlertDialogCancel>
        <Button
          variant="destructive"
          type="button"
          @click="confirmDelete"
          :disabled="deleteLoading"
        >
          <span v-if="deleteLoading" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.actions.confirm') }}
          </span>
          <span v-else>{{ t('modemDetail.actions.confirm') }}</span>
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
