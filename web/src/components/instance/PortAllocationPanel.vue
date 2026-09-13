<script setup lang="ts">
import { computed, watch } from 'vue'
import type { Instance, Port } from '../../api'
import { usePortActions } from '../../composables/usePortActions'
import { summarizePorts } from '../../utils/ports'
import PortAddForm from './PortAddForm.vue'
import PortAllocationTable from './PortAllocationTable.vue'
import PortForwardStatus from './PortForwardStatus.vue'

const props = defineProps<{ instanceId: string; ports: Port[]; status?: string; networkError?: string; disabled: boolean }>()
const emit = defineEmits<{ invalidate: []; refreshed: [value: Instance]; busy: [value: boolean] }>()
const actions = usePortActions(() => props.instanceId, { invalidate: () => emit('invalidate'), refreshed: value => emit('refreshed', value) })
const effectiveStatus = computed(() => actions.confirmed.value ? props.status : 'unverified')
const summary = computed(() => summarizePorts(props.ports))
const disabled = computed(() => props.disabled || actions.busy.value || !['ready', 'inactive'].includes(effectiveStatus.value ?? ''))
const allowDisable = computed(() => !props.disabled && !actions.busy.value && effectiveStatus.value === 'waiting-address')
watch(actions.busy, value => emit('busy', value), { flush: 'sync' })
</script>

<template>
  <section aria-label="端口分配">
    <div class="port-summary">
      <p>可使用号码 <strong class="num">{{ summary.ranges }}</strong>（共 {{ summary.numberCount }} 个号码）</p>
      <p class="port-protocols">TCP <span class="num">{{ summary.tcpRanges }}</span> · UDP <span class="num">{{ summary.hasUDP ? summary.udpRanges : '未分配' }}</span></p>
      <p>{{ effectiveStatus === 'ready' ? '已应用映射' : '设定启用映射' }} {{ summary.enabledCount }} 条。目标端口在映射启用后生效；解除映射后号码仍归本小鸡使用。</p>
    </div>
    <PortForwardStatus :status="effectiveStatus" :error="networkError" :busy="actions.busy.value || props.disabled" @sync="actions.sync" @refresh="actions.refresh" />
    <p v-if="actions.error.value" class="fault" role="alert">{{ actions.error.value }}</p>
    <PortAddForm :disabled="disabled" :udp-allocated="summary.hasUDP" @add="actions.add" />
    <PortAllocationTable :ports="ports" :disabled="disabled" :allow-disable="allowDisable" :applied="effectiveStatus === 'ready'" @edit="actions.edit" @toggle="actions.toggle" />
  </section>
</template>

<style scoped>
.port-summary { margin-bottom: 1rem; }
.port-protocols { color: var(--mute); }
</style>
