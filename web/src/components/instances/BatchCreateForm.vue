<script setup lang="ts">
import { reactive, watch } from 'vue'

const props = defineProps<{ images: string[]; busy: boolean }>()
const emit = defineEmits<{ submit: [Record<string, unknown>]; cancel: [] }>()

const form = reactive({
  name: 'guest',
  count: 1,
  image: 'alpine/3.21/cloud',
  cpuCores: 0.5,
  memoryMib: 128,
  diskGib: 1,
  stackMode: 'v4',
  allocateUDP: false,
  sshPubKey: '',
  passwordLogin: true,
})
watch(() => form.image, (image, previous) => {
  const oldMemory = previous === 'debian/13/cloud' ? 256 : 128
  const oldDisk = previous === 'debian/13/cloud' ? 4 : 1
  if (form.memoryMib === oldMemory) form.memoryMib = image === 'debian/13/cloud' ? 256 : 128
  if (form.diskGib === oldDisk) form.diskGib = image === 'debian/13/cloud' ? 4 : 1
})

function onSubmit() {
  if (!props.busy) emit('submit', { ...form })
}

function onCancel() {
  if (!props.busy) emit('cancel')
}
</script>

<template>
  <form class="form" aria-label="创建小鸡" :aria-busy="busy" @submit.prevent="onSubmit">
    <fieldset class="form-fields" :disabled="busy">
      <label>名称 <input v-model="form.name" required /></label>
      <label>数量 <input v-model.number="form.count" type="number" min="1" max="16" /></label>
      <label>镜像
        <select v-model="form.image">
          <option v-for="img in images" :key="img" :value="img">{{ img }}</option>
        </select>
      </label>
      <label>CPU 核 <input v-model.number="form.cpuCores" type="number" step="0.25" min="0.25" /></label>
      <label>内存 MiB <input v-model.number="form.memoryMib" type="number" min="64" /></label>
      <label>磁盘 GiB <input v-model.number="form.diskGib" type="number" min="1" /></label>
      <label>网络栈
        <select v-model="form.stackMode">
          <option value="v4">IPv4-only</option>
          <option value="v6">IPv6-only</option>
          <option value="dual">双栈</option>
        </select>
      </label>
      <label class="checkbox wide"><input v-model="form.allocateUDP" type="checkbox" /> 同时分配 UDP 端口</label>
      <p class="wide port-help">端口号码分配后供本小鸡使用，默认仅启用首个 TCP 端口到 SSH 22 的映射；其余 TCP 和可选 UDP 端口按需启用。</p>
      <label class="wide">SSH 公钥 <textarea v-model="form.sshPubKey" rows="2" /></label>
      <label><input v-model="form.passwordLogin" type="checkbox" /> 允许密码登录</label>
      <div class="actions">
        <button class="primary" type="submit" :disabled="busy">{{ busy ? '提交中…' : '创建' }}</button>
        <button type="button" :disabled="busy" @click="onCancel">取消</button>
      </div>
    </fieldset>
  </form>
</template>

<style scoped>
.form { max-width: 44rem; }
.form-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 0.7rem; min-width: 0; border: 0; padding: 0; margin: 0; }
.wide { grid-column: 1 / -1; }
label { display: flex; flex-direction: column; gap: 0.25rem; color: var(--mute); font-size: 0.85rem; }
.checkbox { flex-direction: row; align-items: center; gap: .5rem; }
.port-help { margin: 0; color: var(--mute); font-size: .85rem; }
.actions { grid-column: 1 / -1; display: flex; gap: 0.5rem; }
</style>
