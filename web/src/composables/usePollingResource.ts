import { onMounted, onScopeDispose, shallowRef, watch } from 'vue'

export function usePollingResource<T>(
  key: () => string,
  fetchResource: (key: string, signal: AbortSignal) => Promise<T>,
  empty: () => T,
  intervalMs: number,
  shouldPoll: (value: T) => boolean = () => true,
) {
  const data = shallowRef<T>(empty())
  const error = shallowRef('')
  const loading = shallowRef(false)
  let active = false
  let generation = 0
  let controller: AbortController | undefined
  let timer: number | undefined

  function cancel() {
    generation++
    controller?.abort()
    controller = undefined
    if (timer !== undefined) window.clearTimeout(timer)
    timer = undefined
    loading.value = false
  }

  async function refresh() {
    if (!active || !key() || (typeof document !== 'undefined' && document.hidden)) return
    cancel()
    const requestKey = key()
    const version = generation
    const request = new AbortController()
    controller = request
    const current = () => active && version === generation && requestKey === key() && !request.signal.aborted
    loading.value = true
    try {
      const next = await fetchResource(requestKey, request.signal)
      if (current()) {
        data.value = next
        error.value = ''
      }
    } catch (cause) {
      if (current()) error.value = cause instanceof Error ? cause.message : '加载失败'
    } finally {
      if (current()) {
        loading.value = false
        if (intervalMs > 0 && (error.value || shouldPoll(data.value))) {
          timer = window.setTimeout(() => void refresh(), intervalMs)
        }
      }
    }
  }

  watch(key, () => {
    cancel()
    data.value = empty()
    error.value = ''
    void refresh()
  }, { flush: 'sync' })

  function visibilityChanged() {
    if (document.hidden) cancel()
    else void refresh()
  }

  onMounted(() => {
    active = true
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', visibilityChanged)
    void refresh()
  })
  onScopeDispose(() => {
    active = false
    cancel()
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', visibilityChanged)
  })

  return { data, error, loading, refresh }
}
