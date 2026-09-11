<script setup lang="ts">
import HostOverview from '../components/host/HostOverview.vue'
import ResourceCharts from '../components/charts/ResourceCharts.vue'
import { useHost } from '../composables/useHost'
import { usePollingResource } from '../composables/usePollingResource'
import { api, type Metric } from '../api'

const { data, error, load } = useHost()
const { data: series, error: seriesError } = usePollingResource<Metric[]>(() => 'host', async (_key, signal) => (await api.hostSeries(signal)).series ?? [], () => [], 5000)
</script>

<template>
  <div>
    <p v-if="error || seriesError" class="fault">{{ error || seriesError }}</p>
    <HostOverview :snapshot="data" @refresh="load" />
    <h2>CPU 趋势</h2>
    <ResourceCharts :series="series" />
  </div>
</template>

<style scoped>
h2 { font-family: Syne, sans-serif; font-size: 1rem; color: var(--mute); margin-top: 1.5rem; }
</style>
