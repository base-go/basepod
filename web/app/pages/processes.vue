<script setup lang="ts">
interface ProcessInfo {
  id: string
  name: string
  type: 'mlx' | 'mlx-download' | 'container' | 'system'
  status: string
  pid?: number
  port?: number
  model?: string
  image?: string
  cpu?: string
  memory?: string
  uptime?: string
  app_id?: string
  app_name?: string
  progress?: number
}

interface ProcessesResponse {
  processes: ProcessInfo[]
  count: number
}

definePageMeta({
  title: 'Processes'
})

const toast = useToast()

// Fetch processes
const { data, refresh, status: fetchStatus } = await useApiFetch<ProcessesResponse>('/system/processes')

// Auto-refresh every 3 seconds
const refreshInterval = ref<ReturnType<typeof setInterval> | null>(null)
const autoRefresh = ref(true)

onMounted(() => {
  if (autoRefresh.value) {
    refreshInterval.value = setInterval(() => {
      refresh()
    }, 3000)
  }
})

onUnmounted(() => {
  if (refreshInterval.value) {
    clearInterval(refreshInterval.value)
  }
})

// Toggle auto-refresh
function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) {
    refreshInterval.value = setInterval(() => {
      refresh()
    }, 3000)
  } else if (refreshInterval.value) {
    clearInterval(refreshInterval.value)
    refreshInterval.value = null
  }
}

// Process type info
const typeInfo: Record<string, { label: string; icon: string; color: string }> = {
  mlx: { label: 'MLX Server', icon: 'i-heroicons-cpu-chip', color: 'primary' },
  'mlx-download': { label: 'MLX Download', icon: 'i-heroicons-arrow-down-tray', color: 'info' },
  container: { label: 'Container', icon: 'i-heroicons-cube', color: 'success' },
  system: { label: 'System', icon: 'i-heroicons-server', color: 'neutral' },
}

// Status colors
function getStatusColor(status: string): "error" | "primary" | "secondary" | "success" | "info" | "warning" | "neutral" {
  switch (status.toLowerCase()) {
    case 'running':
    case 'completed':
      return 'success'
    case 'downloading':
    case 'generating':
    case 'pending':
      return 'info'
    case 'stopped':
    case 'exited':
      return 'neutral'
    case 'failed':
    case 'error':
      return 'error'
    default:
      return 'neutral'
  }
}

// Stop MLX server
async function stopMLX() {
  try {
    await $api('/mlx/stop', { method: 'POST' })
    toast.add({ title: 'MLX server stopped', color: 'success' })
    refresh()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to stop MLX', description: err.data?.error, color: 'error' })
  }
}

// Stop container
async function stopContainer(containerId: string, name: string) {
  if (!confirm(`Stop container ${name}?`)) return

  try {
    // Find the app by container and stop it
    await $api(`/apps/${containerId}/stop`, { method: 'POST' })
    toast.add({ title: `Stopped ${name}`, color: 'success' })
    refresh()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to stop container', description: err.data?.error, color: 'error' })
  }
}

// Import unmanaged container
const importingContainer = ref<string | null>(null)

async function importContainer(containerId: string, name: string) {
  importingContainer.value = containerId
  try {
    const result = await $api<{ id: string; name: string }>(`/containers/${containerId}/import`, {
      method: 'POST',
      body: { name: name.replace(/^basepod-/, '').replace(/^\//, '') }
    })
    toast.add({ title: `Imported ${result.name}`, description: 'Container is now managed by basepod', color: 'success' })
    refresh()
    // Navigate to the new app
    navigateTo(`/apps/${result.id}`)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to import container', description: err.data?.error, color: 'error' })
  } finally {
    importingContainer.value = null
  }
}

// Group processes by type
const groupedProcesses = computed(() => {
  if (!data.value?.processes) return {}

  const groups: Record<string, ProcessInfo[]> = {}

  for (const proc of data.value.processes) {
    const groupKey = proc.type.includes('download') ? 'downloads' : proc.type
    if (!groups[groupKey]) {
      groups[groupKey] = []
    }
    groups[groupKey].push(proc)
  }

  return groups
})

// Check if there are any processes
const hasProcesses = computed(() => {
  return (data.value?.count || 0) > 0
})
</script>

<template>
  <div>
    <div class="max-w-4xl space-y-6">
      <!-- Header -->
      <div class="flex items-center justify-between">
        <div>
          <h2 class="text-xl font-semibold">Running Processes</h2>
          <p class="text-sm text-gray-500">
            {{ data?.count || 0 }} active process{{ (data?.count || 0) !== 1 ? 'es' : '' }}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <UButton
            :icon="autoRefresh ? 'i-heroicons-pause' : 'i-heroicons-play'"
            variant="soft"
            size="sm"
            @click="toggleAutoRefresh"
          >
            {{ autoRefresh ? 'Pause' : 'Resume' }}
          </UButton>
          <UButton
            icon="i-heroicons-arrow-path"
            variant="soft"
            size="sm"
            :loading="fetchStatus === 'pending'"
            @click="() => refresh()"
          >
            Refresh
          </UButton>
        </div>
      </div>

      <!-- No processes -->
      <UCard v-if="!hasProcesses">
        <div class="text-center py-12">
          <UIcon name="i-heroicons-cpu-chip" class="text-4xl text-gray-400 mb-4" />
          <h3 class="text-lg font-medium mb-2">No Active Processes</h3>
          <p class="text-gray-500">
            Start an LLM model, generate an image, or deploy an app to see processes here.
          </p>
        </div>
      </UCard>

      <!-- MLX Server -->
      <UCard v-if="groupedProcesses.mlx?.length">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon :name="typeInfo.mlx?.icon" class="text-primary-500" />
            <h3 class="font-semibold">MLX LLM Server</h3>
          </div>
        </template>

        <div class="space-y-3">
          <div
            v-for="proc in groupedProcesses.mlx"
            :key="proc.id"
            class="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg"
          >
            <div class="flex items-center gap-4">
              <div>
                <div class="flex items-center gap-2">
                  <p class="font-medium">{{ proc.model || 'MLX Server' }}</p>
                  <UBadge :color="getStatusColor(proc.status)" variant="soft" size="xs">
                    {{ proc.status }}
                  </UBadge>
                </div>
                <p class="text-sm text-gray-500">
                  PID: {{ proc.pid }} · Port: {{ proc.port }}
                </p>
              </div>
            </div>
            <UButton
              color="error"
              variant="soft"
              size="sm"
              @click="stopMLX"
            >
              Stop
            </UButton>
          </div>
        </div>
      </UCard>

      <!-- Downloads -->
      <UCard v-if="groupedProcesses.downloads?.length">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-heroicons-arrow-down-tray" class="text-blue-500" />
            <h3 class="font-semibold">Downloads</h3>
          </div>
        </template>

        <div class="space-y-3">
          <div
            v-for="proc in groupedProcesses.downloads"
            :key="proc.id"
            class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg"
          >
            <div class="flex items-center justify-between mb-2">
              <div class="flex items-center gap-2">
                <p class="font-medium">{{ proc.name }}</p>
                <UBadge color="primary" variant="soft" size="xs">
                  MLX
                </UBadge>
              </div>
              <span class="text-sm text-gray-500">{{ proc.progress }}%</span>
            </div>
            <UProgress
              :value="proc.progress || 0"
              color="primary"
            />
          </div>
        </div>
      </UCard>

      <!-- Containers -->
      <UCard v-if="groupedProcesses.container?.length">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon :name="typeInfo.container?.icon" class="text-green-500" />
            <h3 class="font-semibold">Containers</h3>
            <UBadge color="success" variant="soft" size="xs">
              {{ groupedProcesses.container.length }}
            </UBadge>
          </div>
        </template>

        <div class="divide-y dark:divide-gray-700">
          <div
            v-for="proc in groupedProcesses.container"
            :key="proc.id"
            class="flex items-center justify-between py-3"
          >
            <div>
              <div class="flex items-center gap-2">
                <p class="font-medium">{{ proc.name }}</p>
                <UBadge :color="getStatusColor(proc.status)" variant="soft" size="xs">
                  {{ proc.status }}
                </UBadge>
              </div>
              <p class="text-sm text-gray-500">
                {{ proc.image }}
                <span v-if="proc.app_name" class="text-primary-500">
                  · {{ proc.app_name }}
                </span>
              </p>
              <p class="text-xs text-gray-400 font-mono">{{ proc.id }}</p>
            </div>
            <div class="flex items-center gap-2">
              <NuxtLink
                v-if="proc.app_id"
                :to="`/apps/${proc.app_id}`"
              >
                <UButton variant="soft" size="xs">
                  View App
                </UButton>
              </NuxtLink>
              <UButton
                v-if="proc.app_id"
                color="error"
                variant="soft"
                size="xs"
                @click="stopContainer(proc.app_id, proc.name)"
              >
                Stop
              </UButton>
              <UButton
                v-if="!proc.app_id"
                color="primary"
                variant="soft"
                size="xs"
                :loading="importingContainer === proc.id"
                @click="importContainer(proc.id, proc.name)"
              >
                Import
              </UButton>
            </div>
          </div>
        </div>
      </UCard>

      <!-- Auto-refresh indicator -->
      <div v-if="autoRefresh" class="text-center text-sm text-gray-400">
        <UIcon name="i-heroicons-arrow-path" class="animate-spin mr-1" />
        Auto-refreshing every 3 seconds
      </div>
    </div>
  </div>
</template>
