<script setup lang="ts">
import type { ActivityLog } from '~/types'

definePageMeta({
  title: 'Activity'
})

const activities = ref<ActivityLog[]>([])
const loading = ref(false)
const page = ref(1)
const perPage = 30
const total = ref(0)
const actionFilter = ref('')

const totalPages = computed(() => Math.ceil(total.value / perPage))

async function loadActivities() {
  loading.value = true
  try {
    const offset = (page.value - 1) * perPage
    let url = `/activity?limit=${perPage}&offset=${offset}`
    if (actionFilter.value) url += `&action=${actionFilter.value}`
    const data = await $api<{ activities: ActivityLog[], total: number }>(url)
    activities.value = data.activities || []
    total.value = data.total || 0
  } catch {
    activities.value = []
  } finally {
    loading.value = false
  }
}

function goToPage(p: number) {
  if (p < 1 || p > totalPages.value) return
  page.value = p
  loadActivities()
}

function onFilterChange() {
  page.value = 1
  loadActivities()
}

function getActionIcon(action: string): string {
  if (action.includes('deploy')) return 'i-heroicons-rocket-launch'
  if (action.includes('start')) return 'i-heroicons-play'
  if (action.includes('stop')) return 'i-heroicons-stop'
  if (action.includes('delete') || action.includes('remove')) return 'i-heroicons-trash'
  if (action.includes('create')) return 'i-heroicons-plus'
  if (action.includes('update') || action.includes('config')) return 'i-heroicons-cog-6-tooth'
  if (action.includes('login') || action.includes('auth')) return 'i-heroicons-key'
  if (action.includes('backup')) return 'i-heroicons-cloud-arrow-up'
  if (action.includes('restore')) return 'i-heroicons-arrow-path'
  if (action.includes('invite')) return 'i-heroicons-user-plus'
  return 'i-heroicons-bolt'
}

function getActionColor(action: string): string {
  if (action.includes('delete') || action.includes('remove')) return 'text-red-500'
  if (action.includes('deploy') || action.includes('create')) return 'text-green-500'
  if (action.includes('stop')) return 'text-yellow-500'
  if (action.includes('start')) return 'text-green-500'
  return 'text-gray-400'
}

function formatTime(dateStr: string): string {
  const d = new Date(dateStr)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}d ago`
  return d.toLocaleDateString()
}

onMounted(() => {
  loadActivities()
})
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <div>
        <h2 class="text-xl font-semibold">Activity Log</h2>
        <p class="text-gray-500 dark:text-gray-400">
          All actions across your Basepod instance
          <span v-if="total > 0" class="ml-1">({{ total }} total)</span>
        </p>
      </div>
      <div class="flex items-center gap-2">
        <USelect
          v-model="actionFilter"
          :items="[
            { label: 'All Actions', value: '' },
            { label: 'Deploys', value: 'deploy' },
            { label: 'Start/Stop', value: 'start' },
            { label: 'Config Changes', value: 'config' },
            { label: 'User Actions', value: 'login' }
          ]"
          size="sm"
          @update:model-value="onFilterChange()"
        />
        <UButton icon="i-heroicons-arrow-path" variant="ghost" size="sm" :loading="loading" @click="loadActivities()" />
      </div>
    </div>

    <UCard>
      <div v-if="loading && activities.length === 0" class="flex justify-center py-12">
        <UIcon name="i-heroicons-arrow-path" class="animate-spin text-2xl" />
      </div>

      <div v-else-if="activities.length === 0" class="text-center py-12 text-gray-500">
        <UIcon name="i-heroicons-list-bullet" class="text-4xl mb-2" />
        <p>No activity recorded yet</p>
      </div>

      <div v-else class="divide-y divide-gray-200 dark:divide-gray-800">
        <div v-for="entry in activities" :key="entry.id" class="py-3 flex items-start gap-3">
          <div class="mt-0.5">
            <UIcon :name="getActionIcon(entry.action)" :class="getActionColor(entry.action)" class="w-5 h-5" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-medium text-sm">{{ entry.action }}</span>
              <UBadge v-if="entry.target_name" color="neutral" variant="soft" size="xs">
                {{ entry.target_name }}
              </UBadge>
              <UBadge v-if="entry.status" :color="entry.status === 'success' ? 'success' : entry.status === 'failed' ? 'error' : 'warning'" variant="soft" size="xs">
                {{ entry.status }}
              </UBadge>
              <UBadge color="info" variant="soft" size="xs">{{ entry.actor_type }}</UBadge>
            </div>
            <p v-if="entry.details" class="text-sm text-gray-500 mt-0.5 truncate">{{ entry.details }}</p>
          </div>
          <div class="text-xs text-gray-400 whitespace-nowrap shrink-0">
            {{ formatTime(entry.created_at) }}
          </div>
        </div>
      </div>

      <!-- Pagination -->
      <div v-if="totalPages > 1" class="flex items-center justify-between pt-4 border-t border-gray-200 dark:border-gray-800 mt-2">
        <p class="text-sm text-gray-500">
          Showing {{ (page - 1) * perPage + 1 }}–{{ Math.min(page * perPage, total) }} of {{ total }}
        </p>
        <div class="flex items-center gap-1">
          <UButton
            icon="i-heroicons-chevron-left"
            variant="ghost"
            size="xs"
            :disabled="page <= 1"
            @click="goToPage(page - 1)"
          />
          <template v-for="p in totalPages" :key="p">
            <UButton
              v-if="p === 1 || p === totalPages || (p >= page - 1 && p <= page + 1)"
              size="xs"
              :variant="p === page ? 'solid' : 'ghost'"
              @click="goToPage(p)"
            >
              {{ p }}
            </UButton>
            <span v-else-if="p === page - 2 || p === page + 2" class="text-gray-400 px-1">...</span>
          </template>
          <UButton
            icon="i-heroicons-chevron-right"
            variant="ghost"
            size="xs"
            :disabled="page >= totalPages"
            @click="goToPage(page + 1)"
          />
        </div>
      </div>
    </UCard>
  </div>
</template>
