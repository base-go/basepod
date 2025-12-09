<script setup lang="ts">
import type { HealthResponse, SystemInfoResponse, AppsResponse } from '~/types'

definePageMeta({
  title: 'Dashboard'
})

const { data: health } = await useApiFetch<HealthResponse>('/health')
const { data: systemInfo } = await useApiFetch<SystemInfoResponse>('/system/info')
const { data: apps } = await useApiFetch<AppsResponse>('/apps')

const stats = computed(() => [
  {
    label: 'Total Apps',
    value: apps.value?.total || 0,
    icon: 'i-heroicons-cube',
    color: 'primary' as const
  },
  {
    label: 'Running',
    value: apps.value?.apps?.filter(a => a.status === 'running').length || 0,
    icon: 'i-heroicons-play-circle',
    color: 'success' as const
  },
  {
    label: 'Containers',
    value: systemInfo.value?.containers || 0,
    icon: 'i-heroicons-server',
    color: 'info' as const
  },
  {
    label: 'Images',
    value: systemInfo.value?.images || 0,
    icon: 'i-heroicons-photo',
    color: 'warning' as const
  }
])
</script>

<template>
  <div>
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      <DashboardStatCard
        v-for="stat in stats"
        :key="stat.label"
        :label="stat.label"
        :value="stat.value"
        :icon="stat.icon"
        :color="stat.color"
      />
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <DashboardSystemStatus :health="health" :system-info="systemInfo" />
      <DashboardRecentApps :apps="apps?.apps || []" />
    </div>
  </div>
</template>
