<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import type { Proc } from '../../api'

const props = defineProps<{ processes: Proc[] | null }>()
const emit = defineEmits<{ refresh: [] }>()
const sort = shallowRef<'cpu' | 'rss'>('cpu')

const rows = computed(() => {
  const list = [...(props.processes ?? [])]
  if (sort.value === 'cpu') list.sort((a, b) => b.cpu - a.cpu)
  else list.sort((a, b) => b.rss - a.rss)
  return list
})
</script>

<template>
  <div>
    <div class="bar">
      <button type="button" @click="sort = 'cpu'">按 CPU</button>
      <button type="button" @click="sort = 'rss'">按内存</button>
      <button type="button" @click="emit('refresh')">刷新</button>
    </div>
    <table>
      <thead><tr><th>PID</th><th>名称</th><th>状态</th><th>累计 CPU 秒</th><th>RSS</th></tr></thead>
      <tbody>
        <tr v-for="p in rows" :key="`${p.pid}:${p.start ?? ''}`">
          <td class="num">{{ p.pid }}</td>
          <td>{{ p.name }}</td>
          <td>{{ p.state }}</td>
          <td class="num">{{ p.cpu.toFixed(2) }}</td>
          <td class="num">{{ (p.rss / 1024).toFixed(0) }} KiB</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.bar { display: flex; gap: 0.4rem; margin-bottom: 0.6rem; }
</style>
