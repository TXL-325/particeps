<script setup lang="ts">
import { onMounted, shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import InstanceTable from '../components/instances/InstanceTable.vue'
import BatchCreateForm from '../components/instances/BatchCreateForm.vue'
import { useInstances } from '../composables/useInstances'
import { api } from '../api'

const { instances, selectedIds, error, load } = useInstances()
const router = useRouter()
const showCreate = shallowRef(false)
const images = shallowRef<string[]>(['alpine/3.21/cloud', 'debian/13/cloud'])
const msg = shallowRef('')

onMounted(async () => {
  try {
    images.value = (await api.images()).images.map((i) => i.alias)
  } catch { /* keep defaults */ }
})

async function onCreate(body: Record<string, unknown>) {
  msg.value = ''
  try {
    const task = await api.create(body, crypto.randomUUID())
    showCreate.value = false
    await router.push('/tasks/' + task.id)
  } catch (e) {
    msg.value = e instanceof Error ? e.message : '创建失败'
  }
}

async function batch(op: string) {
  for (const id of selectedIds.value) {
    await api.action(id, op)
  }
  await load()
}
</script>

<template>
  <div>
    <header class="head">
      <h1>小鸡</h1>
      <div class="actions">
        <button class="primary" type="button" @click="showCreate = true">创建</button>
        <button type="button" @click="batch('start')">启动</button>
        <button type="button" @click="batch('stop')">停止</button>
        <button type="button" @click="batch('force-stop')">强制停止</button>
      </div>
    </header>
    <p v-if="error" class="fault">{{ error }}</p>
    <p v-if="msg" class="fault">{{ msg }}</p>
    <BatchCreateForm v-if="showCreate" :images="images" @submit="onCreate" @cancel="showCreate = false" />
    <InstanceTable
      v-else
      :instances="instances"
      :selected-ids="selectedIds"
      @update:selected-ids="selectedIds = $event"
      @open="router.push('/instances/' + $event)"
    />
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
h1 { font-family: Syne, sans-serif; }
.actions { display: flex; gap: 0.4rem; }
</style>
