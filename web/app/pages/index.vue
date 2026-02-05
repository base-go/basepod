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
    description: 'Apps managed by Basepod',
    icon: 'i-heroicons-cube',
    color: 'primary' as const,
    to: '/apps'
  },
  {
    label: 'Running',
    value: apps.value?.apps?.filter(a => a.status === 'running').length || 0,
    description: 'Currently active apps',
    icon: 'i-heroicons-play-circle',
    color: 'success' as const,
    to: '/apps'
  },
  {
    label: 'Stopped',
    value: apps.value?.apps?.filter(a => a.status !== 'running').length || 0,
    description: 'Inactive apps',
    icon: 'i-heroicons-stop-circle',
    color: 'neutral' as const,
    to: '/apps'
  },
  {
    label: 'Images',
    value: systemInfo.value?.images || 0,
    description: 'Container images on system',
    icon: 'i-heroicons-archive-box-arrow-down',
    color: 'warning' as const,
    to: '/container-images'
  }
])
</script>

<template>
  <div>
    <!-- Quick Actions -->
    <div class="flex items-center justify-between mb-6">
      <div>
        <h2 class="text-xl font-semibold">Dashboard</h2>
        <p class="text-gray-500 dark:text-gray-400">Overview of your deployment platform</p>
      </div>
      <div class="flex gap-2">
        <UButton to="/apps" variant="outline" icon="i-heroicons-rectangle-stack">
          View All Apps
        </UButton>
        <UButton to="/templates" icon="i-heroicons-plus">
          Deploy New App
        </UButton>
      </div>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      <DashboardStatCard
        v-for="stat in stats"
        :key="stat.label"
        :label="stat.label"
        :value="stat.value"
        :icon="stat.icon"
        :color="stat.color"
        :to="stat.to"
      />
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <DashboardSystemStatus :health="health" :system-info="systemInfo" />
      <DashboardRecentApps :apps="apps?.apps || []" />
    </div>
  </div>
</template>
