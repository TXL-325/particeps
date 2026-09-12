<script setup lang="ts">
import { shallowRef } from 'vue'

defineProps<{ disabled: boolean }>()
const emit = defineEmits<{ add: [number: number] }>()
const number = shallowRef('')
const error = shallowRef('')
function submit() {
  const requested = number.value.trim() === '' ? 0 : Number(number.value)
  if (!Number.isInteger(requested) || requested < 0 || requested > 65535 || (requested === 0 && number.value.trim() !== '')) {
    error.value = '请输入 1–65535 的整数端口，或留空自动分配。'
    return
  }
  error.value = ''
  emit('add', requested)
}
</script>

<template>
  <form class="port-add" @submit.prevent="submit">
    <fieldset class="port-fields" :disabled="disabled">
      <label>追加端口
        <input :value="number" type="number" min="1" max="65535" step="1" placeholder="留空自动选择" aria-label="追加端口号码"
          @input="number = ($event.target as HTMLInputElement).value" />
      </label>
      <button type="submit">追加 TCP / UDP 端口</button>
    </fieldset>
    <p v-if="error" class="fault" role="alert">{{ error }}</p>
    <p class="port-help">从配置的端口池分配；同一号码的 TCP 和 UDP 一起归属这台小鸡。</p>
  </form>
</template>

<style scoped>
.port-fields { display: flex; flex-wrap: wrap; gap: .6rem; align-items: end; border: 0; margin: 0; padding: 0; }
.port-help { color: var(--mute); font-size: .85rem; }
</style>
