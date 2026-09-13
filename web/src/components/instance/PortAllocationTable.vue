<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import type { Port } from '../../api'
import { isPortEnabled } from '../../utils/ports'

const props = defineProps<{ ports: Port[]; disabled: boolean; allowDisable?: boolean; applied: boolean }>()
const emit = defineEmits<{
  edit: [number: number, proto: string, target: number]
  toggle: [number: number, proto: string, enabled: boolean]
}>()
const editing = shallowRef('')
const target = shallowRef('')
const error = shallowRef('')
const key = (port: Port) => `${port.listenIp}/${port.number}/${port.proto}`
const canToggle = (port: Port) => !props.disabled || (props.allowDisable === true && isPortEnabled(port))
const rows = computed(() => [...props.ports].sort((a, b) => a.number - b.number || a.proto.localeCompare(b.proto)).map(port => {
  const enabled = isPortEnabled(port)
  return {
    ...port, enabled, toggleDisabled: !canToggle(port),
    mappingStatus: props.applied ? (enabled ? '已应用' : '预留 · 未绑定') : (enabled ? '设定启用 · 待确认' : '设定不绑定 · 待确认'),
  }
}))
function begin(port: Port) {
  if (props.disabled) return
  editing.value = key(port)
  target.value = String(port.target)
  error.value = ''
}
function save(port: Port) {
  if (props.disabled) return
  const value = Number(target.value)
  if (!Number.isInteger(value) || value < 1 || value > 65535) {
    error.value = '目标端口必须是 1–65535 的整数。'
    return
  }
  emit('edit', port.number, port.proto, value)
  editing.value = ''
  error.value = ''
}
function toggle(port: Port) {
  if (!canToggle(port)) return
  emit('toggle', port.number, port.proto, !isPortEnabled(port))
}
</script>

<template>
  <div>
    <p v-if="error" class="fault" role="alert">{{ error }}</p>
    <table>
      <thead><tr><th>号码</th><th>协议</th><th>入口地址</th><th>目标端口</th><th>映射状态</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="port in rows" :key="key(port)">
          <td class="num">{{ port.number }}</td>
          <td>{{ port.proto }}</td>
          <td class="num">{{ port.listenIp }}</td>
          <td class="num">
            <template v-if="editing !== key(port)">{{ port.target }} <span v-if="applied && port.enabled && port.proto === 'tcp' && port.target === 22">（SSH）</span></template>
            <input v-else :value="target" :disabled="disabled" type="number" min="1" max="65535" step="1" :aria-label="`${port.number} ${port.proto} 目标端口`"
              @input="target = ($event.target as HTMLInputElement).value" />
          </td>
          <td>{{ port.mappingStatus }}</td>
          <td>
            <div class="port-actions">
              <template v-if="editing !== key(port)">
                <button type="button" :disabled="disabled" @click="begin(port)">编辑目标</button>
                <button type="button" :disabled="port.toggleDisabled" :aria-label="`${port.number} ${port.proto} ${port.enabled ? '解除映射' : '启用映射'}`" @click="toggle(port)">{{ port.enabled ? '解除映射' : '启用映射' }}</button>
              </template>
              <template v-else>
                <button type="button" :disabled="disabled" @click="save(port)">保存目标</button>
                <button type="button" :disabled="disabled" @click="editing = ''">取消</button>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="ports.length === 0" class="port-empty">暂无预留端口。</p>
  </div>
</template>

<style scoped>
.port-empty { color: var(--mute); }
.port-actions { display: flex; flex-wrap: wrap; gap: .4rem; }
</style>
