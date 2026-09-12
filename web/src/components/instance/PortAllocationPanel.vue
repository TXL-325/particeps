<script setup lang="ts">
import { computed, watch } from 'vue'
import type { Instance, Port } from '../../api'
import { usePortActions } from '../../composables/usePortActions'
import PortAddForm from './PortAddForm.vue'
import PortAllocationTable from './PortAllocationTable.vue'
import PortForwardStatus from './PortForwardStatus.vue'

const props = defineProps<{ instanceId: string; ports: Port[]; status?: string; networkError?: string; disabled: boolean }>()
const emit = defineEmits<{ invalidate: []; refreshed: [value: Instance]; busy: [value: boolean] }>()
const actions = usePortActions(() => props.instanceId, { invalidate: () => emit('invalidate'), refreshed: value => emit('refreshed', value) })
const effectiveStatus = computed(() => actions.confirmed.value ? props.status : 'unverified')
const disabled = computed(() => props.disabled || actions.busy.value || !['ready', 'inactive'].includes(effectiveStatus.value ?? ''))
watch(actions.busy, value => emit('busy', value), { flush: 'sync' })
</script>

<template>
  <section aria-label="端口分配">
    <PortForwardStatus :status="effectiveStatus" :error="networkError" :busy="actions.busy.value || props.disabled" @sync="actions.sync" @refresh="actions.refresh" />
    <p v-if="actions.error.value" class="fault" role="alert">{{ actions.error.value }}</p>
    <PortAddForm :disabled="disabled" @add="actions.add" />
    <PortAllocationTable :ports="ports" :disabled="disabled" :applied="effectiveStatus === 'ready'" @edit="actions.edit" />
  </section>
</template>
