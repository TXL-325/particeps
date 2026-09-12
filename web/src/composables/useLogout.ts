import { shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'

export function useLogout() {
  const router = useRouter()
  const busy = shallowRef(false)
  const error = shallowRef('')
  async function logout() {
    if (busy.value) return
    busy.value = true; error.value = ''
    try { await api.logout(); await router.push('/login') }
    catch (cause) { error.value = cause instanceof Error ? cause.message : '退出失败，请重试' }
    finally { busy.value = false }
  }
  return { busy, error, logout }
}
