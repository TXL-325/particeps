<script setup lang="ts">
import { useTokens } from '../../composables/useTokens'
import TokenCreateForm from './TokenCreateForm.vue'
import TokenTable from './TokenTable.vue'
import TokenSecret from './TokenSecret.vue'
const { tokens, loading, creating, revoking, error, secret, create, revoke, reload, clearSecret } = useTokens()
</script>

<template>
  <section aria-labelledby="token-heading">
    <h2 id="token-heading">API Token</h2>
    <p class="mute">只读 Token 用于查询；管理 Token 可修改实例并管理 Token。撤销后立即失效。</p>
    <TokenCreateForm :busy="creating" :disabled="!!revoking" @create="create" />
    <p v-if="error" class="fault" role="alert">{{ error }} <button type="button" :disabled="loading" @click="reload">重试加载</button></p>
    <TokenSecret v-if="secret" :value="secret" @clear="clearSecret" />
    <p v-if="loading" class="mute" role="status">正在加载 Token…</p>
    <TokenTable :tokens="tokens" :busy="creating || !!revoking" :revoking="revoking" @revoke="revoke" />
  </section>
</template>

<style scoped>
h2 { font-family: Syne, sans-serif; font-size: 1rem; margin-top: 1.4rem; }
p.mute { font-size: 0.85rem; }
</style>
