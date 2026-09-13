import { onScopeDispose, readonly, shallowRef, watch } from 'vue'
import { api, type Instance } from '../api'

export function usePortActions(id: () => string, callbacks: { invalidate: () => void; refreshed: (value: Instance) => void }) {
  const busy = shallowRef(false)
  const error = shallowRef('')
  const confirmed = shallowRef(true)
  let version = 0
  watch(id, () => { version++; busy.value = false; error.value = ''; confirmed.value = true }, { flush: 'sync' })
  onScopeDispose(() => { version++ })

  async function run(operation?: (key: string) => Promise<unknown>) {
    if (busy.value) return
    const key = id()
    const current = ++version
    busy.value = true
    confirmed.value = false
    error.value = ''
    callbacks.invalidate()
    const stillCurrent = () => current === version && key === id()
    try {
      await operation?.(key)
    } catch (cause) {
      if (stillCurrent()) error.value = cause instanceof Error ? cause.message : '端口操作失败'
    }
    if (!stillCurrent()) return
    try {
      const next = await api.instance(key)
      if (!stillCurrent()) return
      if (next.id !== key && next.name !== key) throw new Error('端口数据与当前小鸡不一致')
      callbacks.refreshed(next)
      confirmed.value = true
    } catch (cause) {
      if (stillCurrent()) {
        const message = cause instanceof Error ? cause.message : '请求失败'
        error.value = [error.value, `端口状态未能刷新：${message}`].filter(Boolean).join('；')
      }
    } finally {
      if (stillCurrent()) {
        busy.value = false
      }
    }
  }

  return {
    busy: readonly(busy), error: readonly(error), confirmed: readonly(confirmed),
    add: (number: number) => run(key => api.addPort(key, number)),
    edit: (number: number, proto: string, target: number) => run(key => api.editPort(key, number, proto, { target })),
    toggle: (number: number, proto: string, enabled: boolean) => run(key => api.editPort(key, number, proto, { enabled })),
    sync: () => run(key => api.syncPorts(key)),
    refresh: () => run(),
  }
}
