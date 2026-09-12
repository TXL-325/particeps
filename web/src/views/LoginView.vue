<script setup lang="ts">
import { shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'

const password = shallowRef('')
const error = shallowRef('')
const busy = shallowRef(false)
const router = useRouter()

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.login(password.value)
    await router.push('/')
  } catch (e) {
    error.value = e instanceof Error && 'status' in e && e.status === 429
      ? '登录尝试过于频繁，请稍后重试。' : e instanceof Error ? e.message : '登录失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="gate">
    <h1>particeps</h1>
    <p class="mute">母机值班台</p>
    <form @submit.prevent="submit">
      <input v-model="password" type="password" autocomplete="current-password" placeholder="管理员密码" :disabled="busy" />
      <button class="primary" type="submit" :disabled="busy">{{ busy ? '登录中…' : '进入' }}</button>
    </form>
    <p v-if="error" class="fault">{{ error }}</p>
  </div>
</template>

<style scoped>
.gate {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
}
h1 { font-family: Syne, sans-serif; letter-spacing: 0.12em; color: var(--copper); margin: 0; }
form { display: flex; gap: 0.4rem; }
</style>
