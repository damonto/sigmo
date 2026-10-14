import { ref } from 'vue'

const revision = ref(0)

/** sessionRevision lets pending views discard data from a previous login. */
export const sessionRevision = (): number => revision.value

/** invalidateSession invalidates local view state without pretending to revoke a cookie. */
export const invalidateSession = (): void => {
  revision.value += 1
}
