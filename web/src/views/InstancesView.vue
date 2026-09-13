<script setup lang="ts">
import { onMounted, shallowRef } from 'vue'
import { useRouter } from 'vue-router'
import InstanceTable from '../components/instances/InstanceTable.vue'
import BatchCreateForm from '../components/instances/BatchCreateForm.vue'
import { useBatchCreate } from '../composables/useBatchCreate'
import { useInstances } from '../composables/useInstances'
import { api } from '../api'

const { instances, selectedIds, error, load } = useInstances()
const router = useRouter()
const showCreate = shallowRef(false)
const images = shallowRef<string[]>(['alpine/3.21/cloud', 'debian/13/cloud'])
const { busy: creating, error: createError, submit: onCreate } = useBatchCreate(async task => {
  const failure = await router.push('/tasks/' + encodeURIComponent(task.id))
  if (failure) throw new Error('任务已受理，但无法打开任务页面，请重试。')
  showCreate.value = false
})

onMounted(async () => {
  try {
    images.value = (await api.images()).images.map((i) => i.alias)
  } catch { /* keep defaults */ }
})

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
        <button class="primary" type="button" :disabled="creating" @click="showCreate = true">创建</button>
        <button type="button" @click="batch('start')">启动</button>
        <button type="button" @click="batch('stop')">停止</button>
        <button type="button" @click="batch('force-stop')">强制停止</button>
      </div>
    </header>
    <p v-if="error" class="fault">{{ error }}</p>
    <p v-if="createError" class="fault" role="alert">{{ createError }}</p>
    <BatchCreateForm v-if="showCreate" :images="images" :busy="creating" @submit="onCreate" @cancel="showCreate = false" />
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
