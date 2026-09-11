<script setup lang="ts">
import { computed } from 'vue'
import type { Metric } from '../../api'

const props = defineProps<{ series: Metric[] | null }>()

const path = computed(() => {
  const points = props.series ?? []
  if (!points.length) return ''
  const max = points.reduce((current, sample) => sample.quality === 'ok' && Number.isFinite(sample.quotaPercent) ? Math.max(current, sample.quotaPercent) : current, 100)
  const first = points[0]!.ts
  const span = Math.max(points[points.length - 1]!.ts - first, 1)
  let connected = false
  return points.map((s) => {
    if (s.quality !== 'ok' || !Number.isFinite(s.quotaPercent)) { connected = false; return '' }
    const x = ((s.ts - first) / span) * 300
    const y = 80 - (s.quotaPercent / max) * 76
    const command = connected ? 'L' : 'M'
    connected = true
    return `${command}${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
})
</script>

<template>
  <svg viewBox="0 0 300 80" class="chart" role="img" aria-label="CPU 额度使用率，缺测处断开">
    <path :d="path" fill="none" stroke="currentColor" stroke-width="1.5" />
  </svg>
</template>

<style scoped>
.chart { width: 100%; height: 80px; color: var(--copper); }
</style>
