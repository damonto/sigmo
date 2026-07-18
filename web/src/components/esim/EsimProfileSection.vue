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
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useEsimApi } from '@/apis/esim'
import { useReminderApi } from '@/apis/reminder'
import ReminderDialog from '@/components/ReminderDialog.vue'
import ReminderBadge from '@/components/ReminderBadge.vue'
import EsimProfileAvatar from '@/components/esim/EsimProfileAvatar.vue'
import EsimProfileDetailsDialog from '@/components/esim/EsimProfileDetailsDialog.vue'
import type { EsimProfile } from '@/types/esim'
import type { ReminderPayload } from '@/types/reminder'

const profiles = defineModel<EsimProfile[]>('profiles', { required: true })
const props = withDefaults(
  defineProps<{
    modemId: string
    loading?: boolean
    refreshModem?: () => Promise<void>
    internetConnected?: boolean
    internetBusy?: boolean
    wifiCallingAvailable?: boolean
    wifiCallingEnabled?: boolean
    wifiCallingConnected?: boolean
    wifiCallingBusy?: boolean
    simApplicationAvailable?: boolean
    simApplicationProfileIccid?: string
  }>(),
  {
    loading: false,
    internetConnected: false,
    internetBusy: false,
    wifiCallingAvailable: false,
    wifiCallingEnabled: false,
    wifiCallingConnected: false,
    wifiCallingBusy: false,
    simApplicationAvailable: false,
    simApplicationProfileIccid: '',
  },
)
const emit = defineEmits<{
  (event: 'success', message: string): void
  (event: 'toggle-network', profile: EsimProfile, nextValue: boolean): void
  (event: 'toggle-wifi-calling', profile: EsimProfile, nextValue: boolean): void
  (event: 'edit-phone-number', profile: EsimProfile): void
  (event: 'profile-actions-open-change', profile: EsimProfile, open: boolean): void
  (event: 'open-sim-application', profile: EsimProfile): void
}>()

const { t } = useI18n()
const esimApi = useEsimApi()
const reminderApi = useReminderApi()

const profileCount = computed(() => profiles.value.length)
const hasProfiles = computed(() => profiles.value.length > 0)
const isLoading = computed(() => props.loading)
const hasMultipleSEs = computed(
  () => new Set(profiles.value.map((profile) => profile.seId)).size > 1,
)
const profileGroups = computed(() => {
  const groups = new Map<
    string,
    { id: string; label: string; eid?: string; profiles: EsimProfile[] }
  >()
  for (const profile of profiles.value) {
    if (!groups.has(profile.seId)) {
      groups.set(profile.seId, {
        id: profile.seId,
        label: profile.seLabel,
        eid: profile.seEid,
        profiles: [],
      })
    }
    groups.get(profile.seId)?.profiles.push(profile)
  }
  return Array.from(groups.values())
})

const toggleOpen = ref(false)
const toggleProfile = ref<EsimProfile | null>(null)
const toggleNextValue = ref(false)
const toggleLoading = ref(false)

const renameOpen = ref(false)
const renameProfile = ref<EsimProfile | null>(null)

const detailsOpen = ref(false)
const detailsProfile = ref<EsimProfile | null>(null)

const deleteOpen = ref(false)
const deleteProfile = ref<EsimProfile | null>(null)
const deleteLoading = ref(false)

const reminderOpen = ref(false)
const reminderProfile = ref<EsimProfile | null>(null)
const reminderSaving = ref(false)
const reminderDeleting = ref(false)

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

const {
  handleSubmit: handleRenameSubmit,
  resetForm: resetRenameForm,
  isSubmitting: renameSubmitting,
} = useForm<RenameFormValues>({
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
    await esimApi.enableEsim(props.modemId, toggleProfile.value.seId, toggleProfile.value.iccid)
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
      renameProfile.value.seId,
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
    await esimApi.deleteEsim(props.modemId, deleteProfile.value.seId, deleteProfile.value.iccid)
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

const openDetailsDialog = (profile: EsimProfile) => {
  detailsProfile.value = profile
  detailsOpen.value = true
}

const openReminderDialog = (profile: EsimProfile) => {
  reminderProfile.value = profile
  reminderOpen.value = true
}

const closeReminderDialog = () => {
  if (reminderSaving.value || reminderDeleting.value) return
  reminderOpen.value = false
  reminderProfile.value = null
}

const saveReminder = async (payload: ReminderPayload) => {
  const profile = reminderProfile.value
  if (!profile) return
  reminderSaving.value = true
  try {
    const { data } = await reminderApi.saveEsimReminder(
      props.modemId,
      profile.seId,
      profile.iccid,
      payload,
    )
    profile.reminder = data.value ?? {
      nextAt: payload.scheduledAt,
      repeatDays: payload.repeatDays,
      content: payload.content,
    }
    reminderOpen.value = false
    reminderProfile.value = null
    emit('success', t('modemDetail.reminder.saved'))
  } catch (err) {
    console.error('[EsimProfileSection] Failed to save reminder:', err)
  } finally {
    reminderSaving.value = false
  }
}

const clearReminder = async () => {
  const profile = reminderProfile.value
  if (!profile) return
  reminderDeleting.value = true
  try {
    await reminderApi.deleteEsimReminder(props.modemId, profile.seId, profile.iccid)
    profile.reminder = undefined
    reminderOpen.value = false
    reminderProfile.value = null
    emit('success', t('modemDetail.reminder.cleared'))
  } catch (err) {
    console.error('[EsimProfileSection] Failed to clear reminder:', err)
  } finally {
    reminderDeleting.value = false
  }
}

const handleDeleteClick = (profile: EsimProfile) => {
  openDeleteDialog(profile)
}

const handleNetworkToggle = (profile: EsimProfile, nextValue: boolean) => {
  if (!profile.enabled || props.internetBusy) return
  emit('toggle-network', profile, nextValue)
}

const handleWiFiCallingToggle = (profile: EsimProfile, nextValue: boolean) => {
  if (!profile.enabled || props.wifiCallingBusy || !props.wifiCallingAvailable) return
  emit('toggle-wifi-calling', profile, nextValue)
}

const handlePhoneNumberClick = (profile: EsimProfile) => {
  if (!profile.enabled) return
  emit('edit-phone-number', profile)
}

const isProfileSimApplicationAvailable = (profile: EsimProfile) => {
  return (
    props.simApplicationAvailable &&
    profile.enabled &&
    profile.iccid === props.simApplicationProfileIccid
  )
}

const handleSimApplicationClick = (profile: EsimProfile) => {
  if (!isProfileSimApplicationAvailable(profile)) return
  emit('open-sim-application', profile)
}

const handleProfileActionsOpenChange = (profile: EsimProfile, open: boolean) => {
  emit('profile-actions-open-change', profile, open)
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

watch(detailsOpen, (value) => {
  if (value) return
  detailsProfile.value = null
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

    <div v-else class="space-y-4">
      <div v-for="group in profileGroups" :key="group.id" class="space-y-2">
        <div v-if="hasMultipleSEs" class="flex min-w-0 items-center justify-between gap-3 px-1">
          <p class="min-w-0 truncate text-xs font-semibold text-muted-foreground">
            {{ group.label }}: {{ group.eid || 'N/A' }}
          </p>
          <Badge variant="outline" class="shrink-0 text-[10px]">
            {{ group.profiles.length }}
          </Badge>
        </div>
        <div
          v-for="profile in group.profiles"
          :key="profile.id"
          class="rounded-lg border bg-card px-4 py-3 shadow-sm transition"
          :class="profile.enabled ? 'border-primary/40 bg-primary/5' : 'border-transparent'"
        >
          <div class="flex items-center justify-between gap-3">
            <div class="flex min-w-0 items-center gap-3">
              <EsimProfileAvatar
                :name="profile.name"
                :icon="profile.logoUrl"
                :region-code="profile.regionCode"
              />
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
                @update:model-value="(nextValue) => handleToggle(profile, nextValue)"
              />

              <DropdownMenu @update:open="(open) => handleProfileActionsOpenChange(profile, open)">
                <DropdownMenuTrigger as-child>
                  <Button variant="ghost" size="icon" type="button" aria-label="Profile actions">
                    <EllipsisVertical class="size-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" class="w-56">
                  <DropdownMenuItem @click="openReminderDialog(profile)">
                    {{ t('modemDetail.reminder.title') }}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <template v-if="profile.enabled">
                    <DropdownMenuItem
                      class="justify-between gap-4"
                      :disabled="props.internetBusy"
                      @click="handleNetworkToggle(profile, !props.internetConnected)"
                    >
                      <span>{{ t('modemDetail.settings.networkTitle') }}</span>
                      <Switch
                        :model-value="props.internetConnected"
                        :disabled="props.internetBusy"
                        @click.stop
                        @update:model-value="(nextValue) => handleNetworkToggle(profile, nextValue)"
                      />
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      v-if="props.wifiCallingAvailable"
                      class="justify-between gap-4"
                      :disabled="props.wifiCallingBusy"
                      @click="
                        handleWiFiCallingToggle(
                          profile,
                          !(props.wifiCallingEnabled && props.wifiCallingConnected),
                        )
                      "
                    >
                      <span>{{ t('modemDetail.settings.wifiCallingTitle') }}</span>
                      <Spinner
                        v-if="props.wifiCallingBusy"
                        data-testid="wifi-calling-quick-action-loading"
                        class="size-4 text-muted-foreground"
                      />
                      <Switch
                        v-else
                        :model-value="props.wifiCallingEnabled && props.wifiCallingConnected"
                        :disabled="props.wifiCallingBusy"
                        @click.stop
                        @update:model-value="
                          (nextValue) => handleWiFiCallingToggle(profile, nextValue)
                        "
                      />
                    </DropdownMenuItem>
                    <DropdownMenuSeparator v-if="props.wifiCallingAvailable" />
                    <DropdownMenuItem @click="handlePhoneNumberClick(profile)">
                      {{ t('modemDetail.settings.msisdnTitle') }}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator v-if="isProfileSimApplicationAvailable(profile)" />
                    <DropdownMenuItem
                      v-if="isProfileSimApplicationAvailable(profile)"
                      @click="handleSimApplicationClick(profile)"
                    >
                      {{ t('modemDetail.simApplication.title') }}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                  </template>
                  <DropdownMenuItem @click="openDetailsDialog(profile)">
                    {{ t('modemDetail.actions.viewDetails') }}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
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
          <div v-if="profile.reminder" class="mt-3 border-t border-border/60 pt-2">
            <ReminderBadge :reminder="profile.reminder" :profile-name="profile.name" />
          </div>
        </div>
      </div>
    </div>
  </section>

  <ReminderDialog
    v-if="reminderProfile"
    v-model:open="reminderOpen"
    :profile-name="reminderProfile.name"
    :reminder="reminderProfile.reminder"
    :saving="reminderSaving"
    :deleting="reminderDeleting"
    @save="saveReminder"
    @clear="clearReminder"
    @update:open="(value) => !value && closeReminderDialog()"
  />

  <AlertDialog v-model:open="toggleOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ togglePrompt }}</AlertDialogTitle>
        <AlertDialogDescription class="sr-only">
          {{ togglePrompt }}
        </AlertDialogDescription>
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
        <DialogDescription class="sr-only">
          {{ t('modemDetail.esim.nickname') }}
        </DialogDescription>
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

  <EsimProfileDetailsDialog v-model:open="detailsOpen" :profile="detailsProfile" />

  <AlertDialog v-model:open="deleteOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ deletePrompt }}</AlertDialogTitle>
        <AlertDialogDescription class="sr-only">
          {{ deletePrompt }}
        </AlertDialogDescription>
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
