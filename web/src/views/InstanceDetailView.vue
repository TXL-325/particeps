<script setup lang="ts">
import { onScopeDispose, shallowRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { useInstanceDetail } from '../composables/useInstanceDetail'
import ProcessTable from '../components/instance/ProcessTable.vue'
import ResourceCharts from '../components/charts/ResourceCharts.vue'

const props = defineProps<{ id: string }>()
const router = useRouter()
const { inst, procs, series, error, load, loadProcs } = useInstanceDetail(() => props.id)
const actionError = shallowRef('')
const secret = shallowRef('')
const busy = shallowRef(false)
let actionVersion = 0
watch(() => props.id, () => { actionVersion++; secret.value = ''; actionError.value = ''; busy.value = false }, { flush: 'sync' })
onScopeDispose(() => { actionVersion++; secret.value = '' })

async function act(op: string) {
  const id = props.id
  const version = ++actionVersion
  busy.value = true
  actionError.value = ''
  try {
    await api.action(id, op)
    if (version !== actionVersion || props.id !== id) return
    if (op === 'delete') await router.push('/instances')
    else await load()
  } catch (cause) {
    if (version === actionVersion) actionError.value = cause instanceof Error ? cause.message : '操作失败'
  } finally {
    if (version === actionVersion) busy.value = false
  }
}
async function resetPw() {
  const id = props.id
  const version = ++actionVersion
  busy.value = true
  secret.value = ''
  actionError.value = ''
  try {
    const result = await api.password(id)
    if (version === actionVersion && props.id === id) secret.value = result.password
  } catch (cause) {
    if (version === actionVersion) actionError.value = cause instanceof Error ? cause.message : '密码重置失败'
  } finally {
    if (version === actionVersion) busy.value = false
  }
}
function remove() {
  if (window.confirm(`确认删除 ${inst.value?.name ?? props.id} 及其系统盘？`)) void act('delete')
}
</script>

<template>
  <div v-if="inst">
    <header class="head">
      <h1>{{ inst.name }}</h1>
      <fieldset class="actions" :disabled="busy">
        <button type="button" @click="act('start')">启动</button>
        <button type="button" @click="act('stop')">停止</button>
        <button type="button" @click="act('force-stop')">强制停止</button>
        <button type="button" @click="act('restart')">重启</button>
        <button type="button" @click="resetPw">重置密码</button>
        <button type="button" @click="remove">删除</button>
      </fieldset>
    </header>
    <p>{{ inst.status }} · {{ inst.image }} · {{ inst.stackMode }} · {{ inst.natIpv4 }} {{ inst.ipv6 }}</p>
    <p v-if="actionError || error" class="fault">{{ actionError || error }}</p>
    <p v-if="inst.resourceStatus === 'needs-reconciliation'" class="fault">资源修改尚未确认，显示的配额可能尚未更新；核对实际配置后才能继续修改资源。</p>
    <p v-if="secret" class="num">新密码 {{ secret }}（只显示一次）</p>
    <h2>资源</h2>
    <ResourceCharts :series="series" />
    <h2>端口</h2>
    <table>
      <thead><tr><th>号码</th><th>协议</th><th>监听</th><th>目标</th></tr></thead>
      <tbody>
        <tr v-for="p in inst.ports || []" :key="p.number + p.proto">
          <td class="num">{{ p.number }}</td>
          <td>{{ p.proto }}</td>
          <td class="num">{{ p.listenIp }}</td>
          <td class="num">{{ p.target }}</td>
        </tr>
      </tbody>
    </table>
    <h2>内部进程</h2>
    <ProcessTable :processes="procs" @refresh="loadProcs" />
  </div>
  <p v-else-if="error" class="fault">{{ error }}</p>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
h1 { font-family: Syne, sans-serif; margin: 0; }
.actions { display: flex; gap: 0.3rem; flex-wrap: wrap; border: 0; padding: 0; margin: 0; }
h2 { font-size: 1rem; color: var(--mute); margin-top: 1.2rem; }
</style>
