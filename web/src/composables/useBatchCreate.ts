import { onScopeDispose, readonly, shallowRef } from 'vue'
import { api, type Task } from '../api'
import { createRequestKey } from '../utils/requestKey'

export function useBatchCreate(onCreated: (task: Task) => Promise<unknown>) {
  const busy = shallowRef(false)
  const error = shallowRef('')
  let active = true
  const pendingKeys = new Map<string, string>()

  async function submit(body: Record<string, unknown>) {
    if (!active || busy.value) return
    busy.value = true
    error.value = ''
    try {
      const serialized = JSON.stringify(body)
      // Keep every uncertain request, including one restored after editing the form.
      let key = pendingKeys.get(serialized)
      if (!key) {
        key = createRequestKey()
        pendingKeys.set(serialized, key)
      }
      const task = await api.create(body, key)
      if (!active) return
      if (typeof task?.id !== 'string' || !task.id) throw new Error('创建响应缺少任务编号，请重试。')
      await onCreated(task)
      if (active) pendingKeys.delete(serialized)
    } catch (cause) {
      if (active) error.value = cause instanceof Error ? cause.message : '创建失败'
    } finally {
      if (active) busy.value = false
    }
  }

  onScopeDispose(() => { active = false; pendingKeys.clear() })
  return { busy: readonly(busy), error: readonly(error), submit }
}
