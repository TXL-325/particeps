import { api } from '../api'
import { usePollingResource } from './usePollingResource'

export function useHost() {
  const resource = usePollingResource<Record<string, unknown> | null>(() => 'host', (_key, signal) => api.host(signal), () => null, 5000)
  return { data: resource.data, error: resource.error, load: resource.refresh }
}
