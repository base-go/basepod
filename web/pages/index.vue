<script setup lang="ts">
definePageMeta({
  title: 'Dashboard'
})

const { data: health } = await useFetch('/api/health')
const { data: systemInfo } = await useFetch('/api/system/info')
const { data: apps } = await useFetch('/api/apps')

const stats = computed(() => [
  {
    label: 'Total Apps',
    value: apps.value?.total || 0,
    icon: 'i-heroicons-cube',
    color: 'primary'
  },
  {
    label: 'Running',
    value: apps.value?.apps?.filter((a: any) => a.status === 'running').length || 0,
    icon: 'i-heroicons-play-circle',
    color: 'success'
  },
  {
    label: 'Containers',
    value: systemInfo.value?.containers || 0,
    icon: 'i-heroicons-server',
    color: 'info'
  },
  {
    label: 'Images',
    value: systemInfo.value?.images || 0,
    icon: 'i-heroicons-photo',
    color: 'warning'
  }
])

const podmanStatus = computed(() => {
  return health.value?.podman === 'connected' ? 'Connected' : 'Disconnected'
})
</script>

<template>
  <div>
    <template #header>
      Dashboard
    </template>

    <!-- Stats Grid -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      <UCard v-for="stat in stats" :key="stat.label">
        <div class="flex items-center gap-4">
          <div
            class="flex items-center justify-center w-12 h-12 rounded-lg"
            :class="`bg-${stat.color}-100 dark:bg-${stat.color}-900/20`"
          >
            <UIcon
              :name="stat.icon"
              class="w-6 h-6"
              :class="`text-${stat.color}-500`"
            />
          </div>
          <div>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ stat.label }}</p>
            <p class="text-2xl font-bold">{{ stat.value }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Status Section -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- System Status -->
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

      <!-- Recent Apps -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Recent Apps</h3>
            <UButton to="/apps" variant="ghost" size="sm" trailing-icon="i-heroicons-arrow-right">
              View All
            </UButton>
          </div>
        </template>

        <div v-if="apps?.apps?.length" class="space-y-3">
          <div
            v-for="app in apps.apps.slice(0, 5)"
            :key="app.id"
            class="flex items-center justify-between p-3 rounded-lg bg-gray-50 dark:bg-gray-800"
          >
            <div class="flex items-center gap-3">
              <UIcon name="i-heroicons-cube" class="w-5 h-5 text-gray-400" />
              <div>
                <p class="font-medium">{{ app.name }}</p>
                <p class="text-sm text-gray-500">{{ app.domain || 'No domain' }}</p>
              </div>
            </div>
            <UBadge
              :color="app.status === 'running' ? 'success' : app.status === 'stopped' ? 'warning' : 'gray'"
            >
              {{ app.status }}
            </UBadge>
          </div>
        </div>

        <div v-else class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-inbox" class="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>No apps yet</p>
          <UButton to="/apps" variant="soft" class="mt-2">
            Create your first app
          </UButton>
        </div>
      </UCard>
    </div>
  </div>
</template>
