import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

/** Applies image orientation before producing a bounded square PNG. */
export const useAvatarCrop = (selected: (image: Blob) => void) => {
  const { t } = useI18n()
  const preview = ref('')
  const zoom = ref(1)
  const horizontal = ref(50)
  const vertical = ref(50)
  const error = ref('')
  let bitmap: ImageBitmap | undefined
  let generation = 0
  let cropRevision = 0
  const crop = () => {
    if (!bitmap) return
    const canvas = document.createElement('canvas')
    canvas.width = 512
    canvas.height = 512
    const context = canvas.getContext('2d')
    if (!context) return
    const side = Math.min(bitmap.width, bitmap.height) / Number(zoom.value)
    context.drawImage(
      bitmap,
      ((bitmap.width - side) * Number(horizontal.value)) / 100,
      ((bitmap.height - side) * Number(vertical.value)) / 100,
      side,
      side,
      0,
      0,
      512,
      512,
    )
    preview.value = canvas.toDataURL('image/png')
    const current = generation
    const revision = ++cropRevision
    canvas.toBlob((blob) => {
      if (blob && current === generation && revision === cropRevision) selected(blob)
    }, 'image/png')
  }
  const choose = async (event: Event) => {
    const current = ++generation
    const input = event.target
    if (!(input instanceof HTMLInputElement)) return
    const file = input.files?.[0]
    if (!file) return
    error.value = ''
    try {
      if (file.size > 5 * 1024 * 1024) throw new Error(t('communication.avatarTooLarge'))
      const next = await createImageBitmap(file, { imageOrientation: 'from-image' })
      if (current !== generation) {
        next.close()
        return
      }
      bitmap?.close()
      bitmap = next
      zoom.value = 1
      horizontal.value = 50
      vertical.value = 50
      crop()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    }
  }
  watch([zoom, horizontal, vertical], crop)
  onBeforeUnmount(() => {
    generation++
    bitmap?.close()
  })
  return { preview, zoom, horizontal, vertical, error, choose }
}
