<script setup lang="ts">
import type { HealthResponse, SystemInfoResponse } from '~/types'

interface Props {
  health?: HealthResponse | null
  systemInfo?: SystemInfoResponse | null
}

const props = defineProps<Props>()

const podmanStatus = computed(() => {
  return props.health?.podman === 'connected' ? 'Connected' : 'Disconnected'
})
</script>

<template>
  <UCard>
    <template #header>
      <h3 class="font-semibold">System Status</h3>
    </template>

    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <span class="text-gray-600 dark:text-gray-400">Podman</span>
        <UBadge :color="health?.podman === 'connected' ? 'success' : 'error'">
          {{ podmanStatus }}
        </UBadge>
      </div>
      <div class="flex items-center justify-between">
        <span class="text-gray-600 dark:text-gray-400">API</span>
        <UBadge color="success">Running</UBadge>
      </div>
      <div class="flex items-center justify-between">
        <span class="text-gray-600 dark:text-gray-400">Version</span>
        <span class="font-mono text-sm">{{ systemInfo?.version || '0.1.0' }}</span>
      </div>
    </div>
  </UCard>
</template>
