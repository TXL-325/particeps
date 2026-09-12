import { computed } from 'vue'
import { api, type Instance, type Metric, type Proc } from '../api'
import { usePollingResource } from './usePollingResource'

export function useInstanceDetail(id: () => string) {
  const instance = usePollingResource<Instance | null>(id, async (key, signal) => {
    const result = await api.instance(key, signal)
    if (result.id !== key && result.name !== key) throw new Error('实例数据与当前选择不一致')
    return result
  }, () => null, 5000)
  const processes = usePollingResource<Proc[]>(id, async (key, signal) => {
    const response = await api.processes(key, signal)
    let canonical = instance.data.value
    if (response.instanceId !== key && !canonical) {
      canonical = await api.instance(key, signal)
      if (canonical.id !== key && canonical.name !== key) throw new Error('实例数据与当前选择不一致')
    }
    if (response.instanceId !== key && response.instanceId !== canonical?.id) {
      throw new Error('进程数据与所选实例不一致')
    }
    return response.processes ?? []
  }, () => [], 2000)
  const metrics = usePollingResource<Metric[]>(id, async (key, signal) => (await api.instSeries(key, signal)).series ?? [], () => [], 5000)
  const error = computed(() => instance.error.value || processes.error.value || metrics.error.value)
  function invalidatePorts() {
    instance.invalidate()
    if (instance.data.value) instance.data.value = { ...instance.data.value, networkStatus: 'unverified', networkError: '' }
  }
  function acceptInstance(next: Instance) {
    if (next.id !== id() && next.name !== id()) return
    instance.accept(next)
  }
  return { inst: instance.data, procs: processes.data, series: metrics.data, error, load: instance.refresh, loadProcs: processes.refresh, invalidatePorts, acceptInstance }
}
