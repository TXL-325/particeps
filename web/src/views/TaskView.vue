<script setup lang="ts">
import { useRouter } from 'vue-router'
import BatchProgress from '../components/tasks/BatchProgress.vue'
import { useTaskDetails } from '../composables/useTaskDetails'

const props = defineProps<{ id: string }>()
const router = useRouter()
const { batch, error, credentials, credentialError, claiming, claimCredentials, clearCredentials } = useTaskDetails(() => props.id)
</script>

<template>
  <div>
    <h1>任务</h1>
    <p v-if="error || credentialError" class="fault">{{ error || credentialError }}</p>
    <BatchProgress v-if="batch" :batch="batch" :credentials="credentials" :claiming="claiming"
      @claim-credentials="claimCredentials" @clear-credentials="clearCredentials"
      @open-instance="router.push('/instances/' + $event)" />
  </div>
</template>
