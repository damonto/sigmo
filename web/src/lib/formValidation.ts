import { revalidateLogic } from '@tanstack/vue-form'
import type { ValidationLogicFn } from '@tanstack/vue-form'

const validateChange = revalidateLogic({ mode: 'change', modeAfterSubmission: 'change' })
const validateBlur = revalidateLogic({ mode: 'blur', modeAfterSubmission: 'blur' })

/**
 * Runs onDynamic rules on change, blur, and submit. Sharing one error channel
 * lets edits clear errors first reported on blur.
 */
export const validateOnInteraction: ValidationLogicFn = (props) => {
  const validate = props.event.type === 'blur' ? validateBlur : validateChange
  return validate(props)
}
