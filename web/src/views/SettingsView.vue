<script setup lang="ts">
import NetworkSettingsPanel from '../components/settings/NetworkSettingsPanel.vue'
import CPUCapSettings from '../components/settings/CPUCapSettings.vue'
import TokenSettings from '../components/settings/TokenSettings.vue'
</script>

<template>
  <div>
    <h1>设置</h1>
    <NetworkSettingsPanel />
    <CPUCapSettings />
    <TokenSettings />
  </div>
</template>

<style scoped>
h1 { font-family: Syne, sans-serif; }
</style>
