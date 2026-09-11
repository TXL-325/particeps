import { onScopeDispose, shallowRef, watch } from 'vue'
import { api, type InitialCredential, type Task } from '../api'
import { usePollingResource } from './usePollingResource'

export function useTaskDetails(id: () => string) {
  const resource = usePollingResource<Task | null>(id, (key, signal) => api.task(key, signal), () => null, 1500,
    (task) => !task || task.status === 'pending' || task.status === 'running')
  const credentials = shallowRef<InitialCredential[]>([])
  const credentialError = shallowRef('')
  const claiming = shallowRef(false)
  let version = 0

  function clearCredentials() { version++; credentials.value = []; claiming.value = false }
  watch(id, () => { clearCredentials(); credentialError.value = '' }, { flush: 'sync' })
  onScopeDispose(clearCredentials)

  async function claimCredentials() {
    if (claiming.value) return
    const key = id()
    const attempt = ++version
    claiming.value = true
    credentialError.value = ''
    try {
      const result = await api.credentials(key)
      if (attempt !== version || key !== id()) return
      const received = result.credentials ?? []
      const merged = new Map(credentials.value.map(credential => [credential.instanceId, credential]))
      for (const credential of received) merged.set(credential.instanceId, credential)
      credentials.value = [...merged.values()]
      if (!received.length) credentialError.value = '凭据已领取或已过期，可通过实例密码重置设置新密码。'
      await resource.refresh()
    } catch (cause) {
      if (attempt === version) credentialError.value = cause instanceof Error ? cause.message : '领取失败'
    } finally {
      if (attempt === version) claiming.value = false
    }
  }

  return { batch: resource.data, error: resource.error, credentials, credentialError, claiming, claimCredentials, clearCredentials }
}
