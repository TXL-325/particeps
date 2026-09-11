<script setup lang="ts">
import type { InitialCredential, Task } from '../../api'
defineProps<{ batch: Task; credentials: InitialCredential[]; claiming: boolean }>()
defineEmits<{ retry: []; 'open-instance': [string]; 'claim-credentials': []; 'clear-credentials': [] }>()
</script>

<template>
  <div>
    <p>任务 {{ batch.id }} · {{ batch.status }}</p>
    <ul>
      <li v-for="it in batch.items" :key="it.name">
        <strong>{{ it.name }}</strong> {{ it.status }} / {{ it.step }}
        <span v-if="it.error" class="fault"> {{ it.error }}</span>
        <button v-if="it.instanceId" type="button" @click="$emit('open-instance', it.instanceId)">打开</button>
      </li>
    </ul>
    <button v-if="batch.items.some((item) => item.credentialAvailable)" type="button" :disabled="claiming" @click="$emit('claim-credentials')">
      {{ claiming ? '领取中…' : '领取初始凭据（仅一次）' }}
    </button>
    <p v-if="batch.items.some((item) => item.credentialAvailable)">初始凭据可在创建完成后的 15 分钟内领取。</p>
    <div v-if="credentials.length">
      <p>请保存以下凭据，离开此任务页后不会再次显示。</p>
      <p v-for="credential in credentials" :key="credential.instanceId" class="num">{{ credential.name }}：{{ credential.password }}</p>
      <button type="button" @click="$emit('clear-credentials')">清除显示</button>
    </div>
  </div>
</template>
