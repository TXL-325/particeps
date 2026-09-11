<script setup lang="ts">
import { computed } from 'vue'
import type { Instance } from '../../api'

const props = defineProps<{ instances: Instance[]; selectedIds: string[] }>()
const emit = defineEmits<{
  'update:selectedIds': [string[]]
  open: [string]
  action: [string]
}>()

const allSelected = computed(() => props.instances.length > 0 && props.selectedIds.length === props.instances.length)

function toggle(id: string, on: boolean) {
  const next = on ? [...props.selectedIds, id] : props.selectedIds.filter((x) => x !== id)
  emit('update:selectedIds', next)
}
function toggleAll(on: boolean) {
  emit('update:selectedIds', on ? props.instances.map((i) => i.id) : [])
}
function fmt(n: number) {
  return (n / 1024 / 1024).toFixed(1) + ' MiB'
}
</script>

<template>
  <table>
    <thead>
      <tr>
        <th><input type="checkbox" :checked="allSelected" @change="toggleAll(($event.target as HTMLInputElement).checked)" /></th>
        <th>名称</th>
        <th>状态</th>
        <th>CPU</th>
        <th>内存</th>
        <th>网络</th>
        <th>栈</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="row in instances" :key="row.id">
        <td><input type="checkbox" :checked="selectedIds.includes(row.id)" @change="toggle(row.id, ($event.target as HTMLInputElement).checked)" /></td>
        <td><button type="button" class="link" @click="emit('open', row.id)">{{ row.name }}</button></td>
        <td :class="row.status === 'running' ? 'ok' : 'mute'">{{ row.status }}</td>
        <td class="num">{{ row.cpuQuality === 'ok' ? row.cpuUsed.toFixed(2) : '—' }} / {{ row.cpuCores }}</td>
        <td class="num">{{ fmt(row.memUsed) }}</td>
        <td class="num">{{ row.rxBps == null ? '—' : (row.rxBps/1024).toFixed(0) }} / {{ row.txBps == null ? '—' : (row.txBps/1024).toFixed(0) }} KiB/s</td>
        <td>{{ row.stackMode }}</td>
      </tr>
    </tbody>
  </table>
</template>

<style scoped>
.link { background: none; border: 0; color: var(--copper); padding: 0; cursor: pointer; }
</style>
