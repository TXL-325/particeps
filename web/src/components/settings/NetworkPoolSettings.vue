<script setup lang="ts">
import type { NetAddr, Pool } from '../../api'

const props = defineProps<{ detected: NetAddr[]; selected: Pool }>()
const emit = defineEmits<{ save: [Pool] }>()

function toggle(list: string[], ip: string) {
  return list.includes(ip) ? list.filter((x) => x !== ip) : [...list, ip]
}
function onToggleV4(ip: string) {
  emit('save', { ...props.selected, ipv4: toggle(props.selected.ipv4 || [], ip) })
}
function onToggleV6(ip: string) {
  emit('save', { ...props.selected, ipv6: toggle(props.selected.ipv6 || [], ip) })
}
function onTogglePfx(ip: string) {
  emit('save', { ...props.selected, prefixes: toggle(props.selected.prefixes || [], ip) })
}
</script>

<template>
  <div>
    <p class="mute">勾选小鸡可用的地址。NAT 入口与可独占地址在下方指定。</p>
    <ul class="list">
      <li v-for="a in detected" :key="a.ip + a.kind">
        <label>
          <input
            v-if="a.family === 'v4'"
            type="checkbox"
            :checked="(selected.ipv4 || []).includes(a.ip)"
            @change="onToggleV4(a.ip)"
          />
          <input
            v-else-if="a.kind === 'prefix'"
            type="checkbox"
            :checked="(selected.prefixes || []).includes(a.ip)"
            @change="onTogglePfx(a.ip)"
          />
          <input
            v-else
            type="checkbox"
            :checked="(selected.ipv6 || []).includes(a.ip)"
            @change="onToggleV6(a.ip)"
          />
          <span class="num">{{ a.ip }}</span>
          <span class="mute"> {{ a.interface }} · {{ a.kind }}</span>
        </label>
      </li>
    </ul>
    <p v-if="!detected.length" class="mute">未检测到全局地址。</p>
    <div class="row">
      <label>NAT IPv4 <input :value="selected.natIPv4" @change="emit('save', { ...selected, natIPv4: ($event.target as HTMLInputElement).value })" /></label>
      <label>NAT66 IPv6 <input :value="selected.nat66" @change="emit('save', { ...selected, nat66: ($event.target as HTMLInputElement).value })" /></label>
    </div>
  </div>
</template>

<style scoped>
.list { list-style: none; padding: 0; }
.list li { margin: 0.3rem 0; }
.row { display: flex; gap: 1rem; margin-top: 0.8rem; }
label { color: var(--mute); }
</style>
