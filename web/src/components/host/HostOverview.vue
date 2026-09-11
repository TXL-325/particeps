<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ snapshot: Record<string, unknown> | null }>()
defineEmits<{ refresh: [] }>()

const host = computed(() => (props.snapshot?.host ?? {}) as Record<string, number | string>)
const cpuValid = computed(() => (host.value.cpuQuality ?? host.value.quality) === 'ok')
const networkValid = computed(() => (host.value.networkQuality ?? host.value.quality) === 'ok')
const fill = computed(() => cpuValid.value ? Math.min(100, Number(host.value.cpuPercent) || 0) : 0)
const incusLabel = computed(() => {
  const i = props.snapshot?.incus as { ok?: boolean; error?: string } | undefined
  if (!i) return '未知'
  return i.ok ? '已连接' : (i.error || '未连接')
})
const capNote = computed(() => (props.snapshot?.guestCap as { note?: string } | undefined)?.note || '未知')

function formatBytes(n: number) {
  if (!n) return '—'
  const u = ['B', 'KiB', 'MiB', 'GiB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return n.toFixed(1) + ' ' + u[i]
}
function formatBps(n: number) {
  if (!n) return '0'
  return formatBytes(n) + '/s'
}
</script>

<template>
  <div class="overview">
    <header class="head">
      <h1>母机</h1>
      <button type="button" @click="$emit('refresh')">刷新</button>
    </header>
    <div class="bus" aria-label="CPU">
      <div class="fill" :style="{ width: fill + '%' }" />
    </div>
    <dl class="grid">
      <div><dt>CPU</dt><dd class="num">{{ cpuValid ? Number(host.cpuUsedCores).toFixed(2) : '—' }} / {{ host.logicalCpus }} 核</dd></div>
      <div><dt>合计硬上限</dt><dd class="num">{{ snapshot?.cpuCapCores }} 核</dd></div>
      <div><dt>已配置额度</dt><dd class="num">{{ Number(snapshot?.configuredCores || 0).toFixed(2) }} 核</dd></div>
      <div><dt>超分</dt><dd class="num">{{ Number(snapshot?.overcommit || 0).toFixed(2) }}×</dd></div>
      <div><dt>内存可用</dt><dd class="num">{{ formatBytes(Number(host.memAvail)) }}</dd></div>
      <div><dt>磁盘可用</dt><dd class="num">{{ formatBytes(Number(host.diskAvail)) }}</dd></div>
      <div><dt>负载</dt><dd class="num">{{ host.load1 }}</dd></div>
      <div><dt>上行</dt><dd class="num"><template v-if="networkValid">{{ formatBps(Number(host.uplinkRxBps)) }} ↓ {{ formatBps(Number(host.uplinkTxBps)) }} ↑</template><template v-else>—</template></dd></div>
    </dl>
    <p class="mute">总帽：{{ capNote }} · Incus {{ incusLabel }}</p>
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: baseline; }
h1 { font-family: Syne, sans-serif; font-size: 1.6rem; margin: 0 0 0.8rem; }
.bus {
  height: 10px;
  background: #2a241e;
  border: 1px solid var(--copper);
  margin-bottom: 1rem;
}
.fill { height: 100%; background: var(--copper); }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr)); gap: 0.8rem; }
dt { color: var(--mute); font-size: 0.75rem; }
dd { margin: 0.15rem 0 0; }
</style>
