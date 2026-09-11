export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const res = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  if (res.status === 401 && !path.includes('/session')) {
    if (location.pathname !== '/login') location.href = '/login'
  }
  const text = await res.text()
  let data: Record<string, unknown> = {}
  try { data = text ? JSON.parse(text) : {} } catch { throw new ApiError(res.status, res.statusText || '服务器返回了无效响应') }
  if (!res.ok) throw new ApiError(res.status, typeof data.error === 'string' ? data.error : res.statusText)
  return data as T
}

export const api = {
  login: (password: string) => req('/api/v1/session', { method: 'POST', body: JSON.stringify({ password }) }),
  logout: () => req('/api/v1/session', { method: 'DELETE' }),
  host: (signal?: AbortSignal) => req<Record<string, unknown>>('/api/v1/host', { signal }),
  hostSeries: (signal?: AbortSignal) => req<{ series: Metric[] }>('/api/v1/metrics/host', { signal }),
  detected: () => req<{ addresses: NetAddr[] }>('/api/v1/host/network'),
  pool: () => req<Pool>('/api/v1/settings/network-pool'),
  savePool: (p: Pool) => req('/api/v1/settings/network-pool', { method: 'PUT', body: JSON.stringify(p) }),
  setCap: (cores: number) => req('/api/v1/settings/cpu-cap', { method: 'PUT', body: JSON.stringify({ cores }) }),
  instances: (signal?: AbortSignal) => req<{ instances: Instance[] }>('/api/v1/instances', { signal }),
  instance: (id: string, signal?: AbortSignal) => req<Instance>(`/api/v1/instances/${encodeURIComponent(id)}`, { signal }),
  create: (body: unknown, key: string) =>
    req<Task>('/api/v1/instances', { method: 'POST', body: JSON.stringify(body), headers: { 'Idempotency-Key': key } }),
  action: (id: string, op: string) => req(`/api/v1/instances/${id}/${op}`, { method: 'POST', body: '{}' }),
  rebuild: (id: string, image: string) =>
    req(`/api/v1/instances/${id}/rebuild`, { method: 'POST', body: JSON.stringify({ image }) }),
  patch: (id: string, body: unknown) =>
    req(`/api/v1/instances/${id}/resources`, { method: 'PATCH', body: JSON.stringify(body) }),
  password: (id: string) => req<{ password: string }>(`/api/v1/instances/${id}/password`, { method: 'POST', body: '{}' }),
  processes: (id: string, signal?: AbortSignal) => req<{ instanceId: string; processes: Proc[] }>(`/api/v1/instances/${encodeURIComponent(id)}/processes`, { signal }),
  instSeries: (id: string, signal?: AbortSignal) => req<{ series: Metric[] }>(`/api/v1/instances/${encodeURIComponent(id)}/metrics`, { signal }),
  task: (id: string, signal?: AbortSignal) => req<Task>(`/api/v1/tasks/${encodeURIComponent(id)}`, { signal }),
  credentials: (id: string) => req<{ credentials: InitialCredential[] }>(`/api/v1/tasks/${encodeURIComponent(id)}/credentials`, { method: 'POST', body: '{}' }),
  images: () => req<{ images: { alias: string }[] }>('/api/v1/images'),
  tokens: () => req<{ tokens: Token[] }>('/api/v1/tokens'),
  newToken: (name: string, role: string) =>
    req<{ id: string; token: string; role: string }>('/api/v1/tokens', { method: 'POST', body: JSON.stringify({ name, role }) }),
  revoke: (id: string) => req(`/api/v1/tokens/${id}/revoke`, { method: 'POST', body: '{}' }),
}

export interface NetAddr { ip: string; family: string; prefix: number; global: boolean; interface: string; kind: string }
export interface Pool { ipv4: string[]; ipv6: string[]; prefixes: string[]; natIPv4: string; nat66: string; dedicatedV4: string[] }
export interface Port { number: number; proto: string; listenIp: string; target: number }
export interface Instance {
  id: string; name: string; image: string; cpuCores: number; memoryMib: number; diskGib: number
  stackMode: string; desiredPower: string; status: string; cpuUsed: number; memUsed: number
  resourceStatus?: string
  rxBps: number | null; txBps: number | null; cpuQuality?: string; networkQuality?: string; natIpv4: string; ipv6: string; ports?: Port[]
}
export interface Metric { ts: number; cpuCores: number; rxBps: number | null; txBps: number | null; quality: string; networkQuality?: string; quota: number; quotaPercent: number }
export interface Proc { pid: number; name: string; user: string; state: string; start?: number; cpu: number; rss: number }
export interface Task { id: string; kind: string; status: string; items: { name: string; status: string; step: string; error?: string; credentialAvailable?: boolean; instanceId?: string }[] }
export interface InitialCredential { name: string; instanceId: string; password: string }
export interface Token { id: string; name: string; role: string; createdAt: number; revoked: boolean }
