import { toast } from 'vue-sonner'

export const notifyError = (title: string, message: string) => {
  toast.error(title, { description: message })
}
