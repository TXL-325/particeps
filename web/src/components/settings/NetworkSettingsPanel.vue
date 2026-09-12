<script setup lang="ts">
import { shallowRef } from 'vue'
import { api, type NetAddr, type Pool } from '../../api'
import { usePollingResource } from '../../composables/usePollingResource'
import NetworkPoolSettings from './NetworkPoolSettings.vue'
const { data, error, loading, refresh, accept } = usePollingResource<{ detected: NetAddr[]; pool: Pool } | null>(
  () => 'network-settings',
  async () => { const [detected, pool] = await Promise.all([api.detected(), api.pool()]); return { detected: detected.addresses, pool } },
  () => null, 0,
)
const saving = shallowRef(false)
const actionError = shallowRef('')
const message = shallowRef('')
async function save(pool: Pool) {
  if (saving.value || !data.value) return
  saving.value = true; actionError.value = ''; message.value = ''
  try {
    const saved = await api.savePool(pool)
    if (data.value) accept({ ...data.value, pool: saved as Pool })
    message.value = '地址池已保存'
  } catch (cause) { actionError.value = cause instanceof Error ? cause.message : '地址池保存失败' }
  finally { saving.value = false }
}
</script>

<template>
  <section>
    <h2>公网地址池</h2>
    <p v-if="error || actionError" class="fault" role="alert">{{ error || actionError }} <button type="button" :disabled="loading || saving" @click="refresh">重试加载</button></p>
    <p v-if="loading" class="mute">正在加载网络设置…</p>
    <p v-if="message" class="ok" role="status">{{ message }}</p>
    <fieldset v-if="data" :disabled="saving || loading">
      <NetworkPoolSettings :detected="data.detected" :selected="data.pool" @save="save" />
    </fieldset>
  </section>
</template>

<style scoped>
h2 { font-family: Syne, sans-serif; font-size: 1rem; color: var(--mute); margin-top: 1.4rem; }
fieldset { border: 0; margin: 0; padding: 0; }
</style>
