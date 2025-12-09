<script setup lang="ts">
import type { App } from '~/types'

interface Props {
  apps: App[]
  limit?: number
}

const props = withDefaults(defineProps<Props>(), {
  limit: 5
})

const displayedApps = computed(() => props.apps.slice(0, props.limit))
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between">
        <h3 class="font-semibold">Recent Apps</h3>
        <UButton to="/apps" variant="ghost" size="sm" trailing-icon="i-heroicons-arrow-right">
          View All
        </UButton>
      </div>
    </template>

    <div v-if="displayedApps.length" class="space-y-3">
      <div
        v-for="app in displayedApps"
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
          :color="app.status === 'running' ? 'success' : app.status === 'stopped' ? 'warning' : 'neutral'"
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
</template>
