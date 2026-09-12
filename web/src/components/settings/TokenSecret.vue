<script setup lang="ts">
defineProps<{ value: string }>()
defineEmits<{ clear: [] }>()
</script>

<template>
  <div class="token-secret" role="status">
    <p>新 Token 仅在此处交付。请立即保存，离开本页后无法再次查看。</p>
    <code class="num">{{ value }}</code>
    <button type="button" @click="$emit('clear')">清除显示</button>
  </div>
</template>

<style scoped>
.token-secret { border: 1px solid var(--copper); background: var(--panel); padding: 0.9rem; margin: 0.8rem 0; }
p { margin: 0 0 0.6rem; font-size: 0.85rem; color: var(--mute); }
code { display: block; overflow-wrap: anywhere; margin-bottom: 0.7rem; }
</style>
