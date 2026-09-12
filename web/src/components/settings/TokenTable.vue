<script setup lang="ts">
import { shallowRef } from 'vue'
import type { Token } from '../../api'
defineProps<{ tokens: Token[]; busy: boolean; revoking: string }>()
const emit = defineEmits<{ revoke: [id: string] }>()
const confirming = shallowRef('')
function confirm(id: string) { confirming.value = ''; emit('revoke', id) }
</script>

<template>
  <div class="token-list">
    <table>
      <thead><tr><th>名称</th><th>权限</th><th>状态</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="token in tokens" :key="token.id">
          <td>{{ token.name }}</td><td>{{ token.role === 'manage' ? '管理' : '只读' }}</td>
          <td :class="token.revoked ? 'mute' : 'ok'">{{ token.revoked ? '已撤销' : '有效' }}</td>
          <td class="actions">
            <template v-if="!token.revoked">
              <template v-if="confirming === token.id">
                <span class="mute">撤销后立即失效。</span>
                <button type="button" :disabled="busy" @click="confirm(token.id)">确认撤销</button>
                <button type="button" :disabled="busy" @click="confirming = ''">取消</button>
              </template>
              <button v-else type="button" :disabled="busy" :aria-label="'撤销 ' + token.name" @click="confirming = token.id">
                {{ revoking === token.id ? '撤销中…' : '撤销' }}
              </button>
            </template>
            <span v-else class="mute">—</span>
          </td>
        </tr>
        <tr v-if="!tokens.length"><td colspan="4" class="mute">暂无 API Token。</td></tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.token-list { overflow-x: auto; }
.actions { min-width: 7rem; }
.actions button { margin: 0.15rem 0.35rem 0.15rem 0; }
.actions span { margin-right: 0.5rem; }
</style>
