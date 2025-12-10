<script setup lang="ts">
import type { HealthResponse, SystemInfoResponse } from '~/types'

interface VersionInfo {
  current: string
  latest: string
  updateAvailable: boolean
}

interface Props {
  health?: HealthResponse | null
  systemInfo?: SystemInfoResponse | null
}

const props = defineProps<Props>()

const podmanStatus = computed(() => {
  return props.health?.podman === 'connected' ? 'Connected' : 'Disconnected'
})

// Check for updates
const version = ref<VersionInfo | null>(null)
const checkingVersion = ref(false)

const checkVersion = async () => {
  checkingVersion.value = true
  try {
    version.value = await $api<VersionInfo>('/system/version')
  } catch {
    // Ignore errors
  } finally {
    checkingVersion.value = false
  }
}

// Check version on mount
onMounted(() => {
  checkVersion()
})
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between">
        <h3 class="font-semibold">System Status</h3>
        <UBadge v-if="version?.updateAvailable" color="warning">
          Update Available
        </UBadge>
      </div>
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
        <div class="flex items-center gap-2">
          <span class="font-mono text-sm">v{{ version?.current || systemInfo?.version || '0.1.0' }}</span>
          <NuxtLink v-if="version?.updateAvailable" to="/settings" class="text-xs text-primary-500 hover:underline">
            Update to v{{ version?.latest }}
          </NuxtLink>
        </div>
      </div>
    </div>

    <template #footer>
      <NuxtLink to="/settings" class="text-sm text-gray-500 hover:text-primary-500">
        View all settings
      </NuxtLink>
    </template>
  </UCard>
</template>
