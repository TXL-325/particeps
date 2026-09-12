<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import { api } from '../../api'
import { usePollingResource } from '../../composables/usePollingResource'
const { data, error, loading, refresh } = usePollingResource<Record<string, unknown> | null>(
  () => 'cpu-cap', (_, signal) => api.host(signal), () => null, 0,
)
const cap = shallowRef(1.5)
const saving = shallowRef(false)
const actionError = shallowRef('')
const message = shallowRef('')
watch(data, value => { if (value) cap.value = Number(value.cpuCapCores) || 1.5 })
async function save() {
  if (saving.value || !data.value) return
  saving.value = true; actionError.value = ''; message.value = ''
  try { await api.setCap(cap.value); message.value = '合计硬上限已更新' }
  catch (cause) { actionError.value = cause instanceof Error ? cause.message : '硬上限更新失败' }
  finally { saving.value = false }
}
</script>

<template>
  <section>
    <h2>小鸡合计 CPU 硬上限</h2>
    <p v-if="error || actionError" class="fault" role="alert">{{ error || actionError }} <button type="button" :disabled="loading || saving" @click="refresh">重试加载</button></p>
    <p v-if="message" class="ok" role="status">{{ message }}</p>
    <form @submit.prevent="save">
      <fieldset :disabled="loading || saving || !data">
        <label>核 <input v-model.number="cap" type="number" step="0.25" min="0.25" /></label>
        <button class="primary" type="submit">{{ saving ? '应用中…' : '应用' }}</button>
      </fieldset>
    </form>
  </section>
</template>

<style scoped>
h2 { font-family: Syne, sans-serif; font-size: 1rem; color: var(--mute); margin-top: 1.4rem; }
fieldset { display: flex; flex-wrap: wrap; gap: 0.5rem; border: 0; margin: 0; padding: 0; }
</style>
