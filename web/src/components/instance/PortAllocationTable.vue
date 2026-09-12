<script setup lang="ts">
import { shallowRef } from 'vue'
import type { Port } from '../../api'

defineProps<{ ports: Port[]; disabled: boolean; applied: boolean }>()
const emit = defineEmits<{ edit: [number: number, proto: string, target: number] }>()
const editing = shallowRef('')
const target = shallowRef('')
const error = shallowRef('')
const key = (port: Port) => `${port.listenIp}/${port.number}/${port.proto}`
function begin(port: Port) {
  editing.value = key(port)
  target.value = String(port.target)
  error.value = ''
}
function save(port: Port) {
  const value = Number(target.value)
  if (!Number.isInteger(value) || value < 1 || value > 65535) {
    error.value = '目标端口必须是 1–65535 的整数。'
    return
  }
  emit('edit', port.number, port.proto, value)
  editing.value = ''
  error.value = ''
}
</script>

<template>
  <div>
    <p v-if="error" class="fault" role="alert">{{ error }}</p>
    <table>
      <thead><tr><th>号码</th><th>协议</th><th>入口地址</th><th>目标端口</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="port in ports" :key="key(port)">
          <td class="num">{{ port.number }}</td>
          <td>{{ port.proto }}</td>
          <td class="num">{{ port.listenIp }}</td>
          <td class="num">
            <template v-if="editing !== key(port)">{{ port.target }} <span v-if="applied && port.proto === 'tcp' && port.target === 22">（SSH）</span></template>
            <input v-else :value="target" :disabled="disabled" type="number" min="1" max="65535" step="1" :aria-label="`${port.number} ${port.proto} 目标端口`"
              @input="target = ($event.target as HTMLInputElement).value" />
          </td>
          <td>
            <button v-if="editing !== key(port)" type="button" :disabled="disabled" @click="begin(port)">编辑目标</button>
            <template v-else>
              <button type="button" :disabled="disabled" @click="save(port)">保存目标</button>
              <button type="button" :disabled="disabled" @click="editing = ''">取消</button>
            </template>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="ports.length === 0" class="port-empty">暂无已分配端口。</p>
  </div>
</template>

<style scoped>
.port-empty { color: var(--mute); }
</style>
