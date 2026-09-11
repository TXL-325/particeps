import { ref } from 'vue'
import { api, type Instance } from '../api'
import { usePollingResource } from './usePollingResource'

export function useInstances() {
  const resource = usePollingResource<Instance[]>(() => 'instances', async (_key, signal) => (await api.instances(signal)).instances ?? [], () => [], 5000)
  const selectedIds = ref<string[]>([])
  return { instances: resource.data, selectedIds, error: resource.error, load: resource.refresh }
}
