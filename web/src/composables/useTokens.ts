import { onMounted, onScopeDispose, shallowRef } from 'vue'
import { api, type Token } from '../api'

export function useTokens() {
  const tokens = shallowRef<Token[]>([])
  const loading = shallowRef(false)
  const creating = shallowRef(false)
  const revoking = shallowRef('')
  const error = shallowRef('')
  const secret = shallowRef('')
  let secretID = ''
  let active = true
  let readVersion = 0

  function clearSecret() { secret.value = ''; secretID = '' }
  async function refresh() {
    const version = ++readVersion
    loading.value = true
    try {
      const result = await api.tokens()
      if (!Array.isArray(result.tokens)) throw new Error('Token 列表响应无效，请重试')
      if (active && version === readVersion) {
        tokens.value = result.tokens
        if (secretID && result.tokens.some(token => token.id === secretID && token.revoked)) clearSecret()
      }
      return true
    } catch (cause) {
      if (active && version === readVersion) error.value = cause instanceof Error ? cause.message : 'Token 列表加载失败'
      return false
    } finally {
      if (active && version === readVersion) loading.value = false
    }
  }
  async function reload() { error.value = ''; await refresh() }
  async function create(name: string, role: 'read' | 'manage') {
    if (!active || creating.value || revoking.value) return
    creating.value = true
    error.value = ''
    try {
      const result = await api.newToken(name, role)
      if (!active) return
      secret.value = result.token
      secretID = result.id
      await refresh()
    } catch (cause) {
      if (active) error.value = cause instanceof Error ? cause.message : 'Token 签发失败'
    } finally {
      if (active) creating.value = false
    }
  }
  async function revoke(id: string) {
    if (!active || creating.value || revoking.value) return
    revoking.value = id
    error.value = ''
    try {
      await api.revoke(id)
      if (!active) return
      // The write response confirms revocation even if the following read fails.
      tokens.value = tokens.value.map(token => token.id === id ? { ...token, revoked: true } : token)
      if (secretID === id) clearSecret()
      await refresh()
    } catch (cause) {
      if (active) {
        const message = cause instanceof Error ? cause.message : 'Token 撤销失败'
        await refresh()
        if (active) error.value = message
      }
    } finally {
      if (active) revoking.value = ''
    }
  }
  onMounted(() => void refresh())
  onScopeDispose(() => { active = false; readVersion++; clearSecret() })
  return { tokens, loading, creating, revoking, error, secret, create, revoke, reload, clearSecret }
}
