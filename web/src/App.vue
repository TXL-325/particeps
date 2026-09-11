<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { shallowRef } from 'vue'
import { api } from './api'

const route = useRoute()
const router = useRouter()
const hideNav = computed(() => route.path === '/login')

const authed = shallowRef(true)
async function logout() {
  await api.logout()
  authed.value = false
  await router.push('/login')
}
</script>

<template>
  <div class="shell" :class="{ login: hideNav }">
    <aside v-if="!hideNav" class="spine">
      <span class="brand">particeps</span>
    </aside>
    <div class="main">
      <nav v-if="!hideNav" class="top">
        <RouterLink to="/">概览</RouterLink>
        <RouterLink to="/instances">小鸡</RouterLink>
        <RouterLink to="/settings">设置</RouterLink>
        <span class="spacer" />
        <button type="button" @click="logout">退出</button>
      </nav>
      <section class="page">
        <RouterView />
      </section>
    </div>
  </div>
</template>

<style scoped>
.shell { display: flex; min-height: 100%; }
.spine {
  width: var(--rail);
  background: #151210;
  border-right: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
}
.brand {
  font-family: Syne, sans-serif;
  font-weight: 800;
  letter-spacing: 0.18em;
  writing-mode: vertical-rl;
  transform: rotate(180deg);
  color: var(--copper);
  font-size: 0.95rem;
}
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.top {
  display: flex; gap: 1rem; align-items: center;
  padding: 0.7rem 1.2rem;
  border-bottom: 1px solid var(--line);
}
.top a { color: var(--mute); }
.top a.router-link-active { color: var(--paper); }
.spacer { flex: 1; }
.page { padding: 1.2rem; flex: 1; }
.login .page { padding: 0; }
</style>
