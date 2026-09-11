<script setup lang="ts">
import { onMounted, reactive, ref, shallowRef } from 'vue'
import { api, type NetAddr, type Pool, type Token } from '../api'
import NetworkPoolSettings from '../components/settings/NetworkPoolSettings.vue'

const detected = ref<NetAddr[]>([])
const pool = ref<Pool>({ ipv4: [], ipv6: [], prefixes: [], natIPv4: '', nat66: '', dedicatedV4: [] })
const cap = shallowRef(1.5)
const tokens = ref<Token[]>([])
const tokenPlain = shallowRef('')
const newTok = reactive({ name: 'api', role: 'manage' })
const msg = shallowRef('')

onMounted(async () => {
  detected.value = (await api.detected()).addresses
  pool.value = await api.pool()
  const h = await api.host()
  cap.value = Number(h.cpuCapCores) || 1.5
  tokens.value = (await api.tokens()).tokens
})

async function savePool(p: Pool) {
  pool.value = await api.savePool(p) as Pool
  pool.value = p
  msg.value = '地址池已保存'
}
async function saveCap() {
  await api.setCap(cap.value)
  msg.value = '合计硬上限已更新'
}
async function makeToken() {
  const r = await api.newToken(newTok.name, newTok.role)
  tokenPlain.value = r.token
  tokens.value = (await api.tokens()).tokens
}
</script>

<template>
  <div>
    <h1>设置</h1>
    <p v-if="msg" class="ok">{{ msg }}</p>
    <h2>公网地址池</h2>
    <NetworkPoolSettings :detected="detected" :selected="pool" @save="savePool" />
    <h2>小鸡合计 CPU 硬上限</h2>
    <label>核 <input v-model.number="cap" type="number" step="0.25" min="0.25" /></label>
    <button class="primary" type="button" @click="saveCap">应用</button>
    <h2>API Token</h2>
    <form class="tok" @submit.prevent="makeToken">
      <input v-model="newTok.name" placeholder="名称" />
      <select v-model="newTok.role">
        <option value="read">只读</option>
        <option value="manage">管理</option>
      </select>
      <button class="primary" type="submit">签发</button>
    </form>
    <p v-if="tokenPlain" class="num">新 Token {{ tokenPlain }}（只显示一次）</p>
    <table>
      <thead><tr><th>名称</th><th>角色</th><th>状态</th></tr></thead>
      <tbody>
        <tr v-for="t in tokens" :key="t.id">
          <td>{{ t.name }}</td>
          <td>{{ t.role }}</td>
          <td>{{ t.revoked ? '已撤销' : '有效' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
h1, h2 { font-family: Syne, sans-serif; }
h2 { font-size: 1rem; color: var(--mute); margin-top: 1.4rem; }
.tok { display: flex; gap: 0.4rem; margin: 0.5rem 0; }
</style>
