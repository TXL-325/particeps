<script setup lang="ts">
import { reactive } from 'vue'
const props = defineProps<{ busy: boolean; disabled: boolean }>()
const emit = defineEmits<{ create: [name: string, role: 'read' | 'manage'] }>()
const form = reactive({ name: 'api', role: 'manage' as 'read' | 'manage' })
function submit() {
  if (!props.busy && !props.disabled && form.name.trim()) emit('create', form.name.trim(), form.role)
}
</script>

<template>
  <form class="token-form" aria-label="签发 API Token" @submit.prevent="submit">
    <fieldset :disabled="busy || disabled">
      <label>名称 <input v-model="form.name" required maxlength="100" autocomplete="off" placeholder="例如：自动化管理" /></label>
      <label>权限
        <select v-model="form.role"><option value="read">只读</option><option value="manage">管理</option></select>
      </label>
      <button class="primary" type="submit" :disabled="busy || disabled">{{ busy ? '签发中…' : '签发' }}</button>
    </fieldset>
  </form>
</template>

<style scoped>
fieldset { display: flex; align-items: end; flex-wrap: wrap; gap: 0.65rem; border: 0; padding: 0; margin: 0.75rem 0; }
label { display: grid; gap: 0.35rem; color: var(--mute); font-size: 0.85rem; }
</style>
