<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status?: string; error?: string; busy: boolean }>()
defineEmits<{ sync: []; refresh: [] }>()
const message = computed(() => {
  switch (props.status) {
    case 'unverified': return '最新端口状态尚未确认，下表保留上次读取的数据。请刷新状态后再操作。'
    case 'ready': return '已启用的端口规则已应用，其余预留端口未绑定；外部能否访问还取决于防火墙、路由和小鸡中的服务。'
    case 'inactive': return '小鸡的端口分配和目标已保留，转发当前未启用；启动后会重新应用。'
    case 'waiting-address': return '小鸡尚未取得可用的内部 IPv4，转发已暂停；取得地址后会恢复，端口分配仍被保留。'
    case 'cleanup-pending': return '删除尚未完成，端口仍被保留。请使用上方“删除”重试清理。'
    case 'needs-reconciliation': return '上次写入的结果不明，端口仍被保留。需先核对后端操作，才能继续修改或删除。'
    case 'pending': return '下表为已保存的预留与映射设置，转发尚未确认生效，旧规则可能仍然存在。请重新应用规则。'
    default: return '现有端口规则尚未核对；应用成功后才会显示为已应用。'
  }
})
const retryable = computed(() => props.status !== 'cleanup-pending' && props.status !== 'needs-reconciliation')
</script>

<template>
  <div class="port-status" aria-live="polite">
    <p>{{ message }}</p>
    <p v-if="error" class="fault">{{ error }}</p>
    <button v-if="status === 'unverified'" type="button" :disabled="busy" @click="$emit('refresh')">{{ busy ? '正在核对…' : '刷新端口状态' }}</button>
    <button v-else-if="retryable" type="button" :disabled="busy" @click="$emit('sync')">{{ busy ? '处理中…' : '重新应用端口规则' }}</button>
  </div>
</template>

<style scoped>
.port-status { margin-bottom: 1rem; }
</style>
