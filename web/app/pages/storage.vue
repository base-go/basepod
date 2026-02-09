<script setup lang="ts">
// --- Types ---
interface DiskInfo {
  total: number
  used: number
  available: number
  percent: number
  formatted: {
    total: string
    used: string
    available: string
  }
}

interface StorageCategory {
  id: string
  name: string
  size: number
  formatted: string
  count: number
  icon: string
  color: string
}

interface SystemStorageResponse {
  disk: DiskInfo
  categories: StorageCategory[]
  basepod_total: number
  basepod_formatted: string
  other_size: number
  other_formatted: string
}

interface FluxStorageItem {
  path: string
  size: number
  formatted: string
  count: number
}

interface FluxStorageInfo {
  models: FluxStorageItem
  generations: FluxStorageItem
  uploads: FluxStorageItem
  venv: FluxStorageItem
  total: number
  total_formatted: string
}

interface StorageFile {
  id: string
  name: string
  path: string
  size: number
  formatted: string
  type: string
  created_at?: string
}

definePageMeta({
  title: 'Storage'
})

const toast = useToast()

// --- Fetch system storage overview ---
const { data: systemStorage, refresh: refreshSystemStorage } = await useApiFetch<SystemStorageResponse>('/system/storage')

// --- FLUX drill-down state ---
const fluxExpanded = ref(false)
const fluxStorage = ref<FluxStorageInfo | null>(null)
const fluxLoading = ref(false)
const selectedFluxCategory = ref<string | null>(null)
const fluxFiles = ref<StorageFile[]>([])
const loadingFluxFiles = ref(false)

// Color maps — explicit classes (no dynamic Tailwind interpolation)
const bgColorMap: Record<string, string> = {
  blue: 'bg-blue-500',
  cyan: 'bg-cyan-500',
  green: 'bg-green-500',
  amber: 'bg-amber-500',
  orange: 'bg-orange-500',
  purple: 'bg-purple-500',
  pink: 'bg-pink-500',
  gray: 'bg-gray-400 dark:bg-gray-500'
}

const dotColorMap: Record<string, string> = {
  blue: 'bg-blue-500',
  cyan: 'bg-cyan-500',
  green: 'bg-green-500',
  amber: 'bg-amber-500',
  orange: 'bg-orange-500',
  purple: 'bg-purple-500',
  pink: 'bg-pink-500',
  gray: 'bg-gray-400'
}

const borderColorMap: Record<string, string> = {
  blue: 'border-l-blue-500',
  cyan: 'border-l-cyan-500',
  green: 'border-l-green-500',
  amber: 'border-l-amber-500',
  orange: 'border-l-orange-500',
  purple: 'border-l-purple-500',
  pink: 'border-l-pink-500',
  gray: 'border-l-gray-400'
}

const iconBgMap: Record<string, string> = {
  blue: 'bg-blue-100 dark:bg-blue-900/50 text-blue-500',
  cyan: 'bg-cyan-100 dark:bg-cyan-900/50 text-cyan-500',
  green: 'bg-green-100 dark:bg-green-900/50 text-green-500',
  amber: 'bg-amber-100 dark:bg-amber-900/50 text-amber-500',
  orange: 'bg-orange-100 dark:bg-orange-900/50 text-orange-500',
  purple: 'bg-purple-100 dark:bg-purple-900/50 text-purple-500',
  pink: 'bg-pink-100 dark:bg-pink-900/50 text-pink-500',
  gray: 'bg-gray-100 dark:bg-gray-700 text-gray-500'
}

// --- Disk bar segments ---
const barSegments = computed(() => {
  if (!systemStorage.value) return []
  const disk = systemStorage.value.disk
  if (!disk.total) return []

  const segments: { label: string; size: number; formatted: string; color: string; pct: number }[] = []

  // Basepod categories
  for (const cat of systemStorage.value.categories) {
    if (cat.size > 0) {
      segments.push({
        label: cat.name,
        size: cat.size,
        formatted: cat.formatted,
        color: cat.color,
        pct: (cat.size / disk.total) * 100
      })
    }
  }

  // Other / System
  if (systemStorage.value.other_size > 0) {
    segments.push({
      label: 'Other / System',
      size: systemStorage.value.other_size,
      formatted: systemStorage.value.other_formatted,
      color: 'slate',
      pct: (systemStorage.value.other_size / disk.total) * 100
    })
  }

  return segments
})

const availablePct = computed(() => {
  if (!systemStorage.value?.disk.total) return 0
  return (systemStorage.value.disk.available / systemStorage.value.disk.total) * 100
})

// --- Toggle FLUX drill-down ---
async function toggleFlux() {
  fluxExpanded.value = !fluxExpanded.value
  if (fluxExpanded.value && !fluxStorage.value) {
    fluxLoading.value = true
    try {
      fluxStorage.value = await $api<FluxStorageInfo>('/flux/storage')
    } catch {
      fluxStorage.value = null
    } finally {
      fluxLoading.value = false
    }
  }
}

// --- FLUX file list ---
const fluxCategories = computed(() => {
  if (!fluxStorage.value) return []
  return [
    { id: 'models', name: 'Models', icon: 'i-heroicons-cpu-chip', ...fluxStorage.value.models, color: 'purple' },
    { id: 'generations', name: 'Generated Images', icon: 'i-heroicons-photo', ...fluxStorage.value.generations, color: 'green' },
    { id: 'uploads', name: 'Uploads', icon: 'i-heroicons-arrow-up-tray', ...fluxStorage.value.uploads, color: 'amber' },
    { id: 'venv', name: 'Python Environment', icon: 'i-heroicons-code-bracket', ...fluxStorage.value.venv, color: 'gray', readonly: true }
  ]
})

async function loadFluxFiles(category: string) {
  if (selectedFluxCategory.value === category) {
    selectedFluxCategory.value = null
    fluxFiles.value = []
    return
  }
  loadingFluxFiles.value = true
  selectedFluxCategory.value = category
  try {
    fluxFiles.value = await $api<StorageFile[]>(`/flux/storage/${category}`)
  } catch {
    fluxFiles.value = []
  } finally {
    loadingFluxFiles.value = false
  }
}

async function deleteFile(file: StorageFile) {
  if (!confirm(`Delete ${file.name}? This cannot be undone.`)) return
  try {
    if (file.type === 'models') {
      await $api(`/flux/models/${file.id}`, { method: 'DELETE' })
    } else if (file.type === 'generations') {
      await $api(`/flux/generations/${file.id}`, { method: 'DELETE' })
    }
    toast.add({ title: 'Deleted successfully', color: 'success' })
    // Refresh both
    await refreshSystemStorage()
    fluxStorage.value = await $api<FluxStorageInfo>('/flux/storage')
    if (selectedFluxCategory.value) {
      fluxFiles.value = await $api<StorageFile[]>(`/flux/storage/${selectedFluxCategory.value}`)
    }
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to delete', description: err.data?.error, color: 'error' })
  }
}
</script>

<template>
  <div>
    <!-- Header -->
    <div class="mb-6">
      <h2 class="text-xl font-semibold">Storage</h2>
      <p class="text-gray-500 dark:text-gray-400">
        Disk usage overview
      </p>
    </div>

    <!-- Section 1: Disk Overview Bar -->
    <div class="mb-6 p-5 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
      <div class="flex items-center justify-between mb-4">
        <div>
          <span class="text-2xl font-bold">{{ systemStorage?.disk.formatted.used || '—' }}</span>
          <span class="text-gray-500 dark:text-gray-400 ml-1">of {{ systemStorage?.disk.formatted.total || '—' }} used</span>
        </div>
        <span class="text-sm text-gray-500 dark:text-gray-400">
          {{ systemStorage?.disk.formatted.available || '—' }} available
        </span>
      </div>

      <!-- macOS-style disk bar -->
      <div class="h-6 bg-gray-200 dark:bg-gray-700 rounded-lg overflow-hidden flex">
        <div
          v-for="(seg, i) in barSegments"
          :key="seg.label"
          class="h-full transition-all duration-300"
          :class="[
            seg.color === 'slate' ? 'bg-gray-400 dark:bg-gray-500' : (bgColorMap[seg.color] || 'bg-gray-400'),
            i === 0 ? 'rounded-l-lg' : ''
          ]"
          :style="{ width: `${seg.pct}%`, minWidth: seg.pct > 0.3 ? '3px' : '0' }"
          :title="`${seg.label}: ${seg.formatted}`"
        />
      </div>

      <!-- Legend -->
      <div class="flex flex-wrap gap-x-5 gap-y-2 mt-4">
        <div v-for="seg in barSegments" :key="seg.label" class="flex items-center gap-1.5 text-sm">
          <span
            class="w-2.5 h-2.5 rounded-full"
            :class="seg.color === 'slate' ? 'bg-gray-400' : (dotColorMap[seg.color] || 'bg-gray-400')"
          />
          <span class="text-gray-600 dark:text-gray-400">{{ seg.label }}</span>
          <span class="font-medium">{{ seg.formatted }}</span>
        </div>
        <div class="flex items-center gap-1.5 text-sm">
          <span class="w-2.5 h-2.5 rounded-full bg-gray-200 dark:bg-gray-700" />
          <span class="text-gray-600 dark:text-gray-400">Available</span>
          <span class="font-medium">{{ systemStorage?.disk.formatted.available || '—' }}</span>
        </div>
      </div>
    </div>

    <!-- Section 2: Basepod Categories Grid -->
    <div class="mb-6">
      <h3 class="text-sm font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-3">Basepod Storage</h3>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <button
          v-for="cat in systemStorage?.categories || []"
          :key="cat.id"
          class="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4 flex items-center gap-3 border-l-4 text-left transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
          :class="[
            borderColorMap[cat.color] || 'border-l-gray-400',
            cat.id === 'flux' && fluxExpanded ? 'ring-2 ring-purple-500/30' : ''
          ]"
          @click="cat.id === 'flux' ? toggleFlux() : undefined"
        >
          <div
            class="w-10 h-10 rounded-lg flex items-center justify-center shrink-0"
            :class="iconBgMap[cat.color] || 'bg-gray-100 dark:bg-gray-700 text-gray-500'"
          >
            <UIcon :name="cat.icon" class="w-5 h-5" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="font-medium truncate">{{ cat.name }}</div>
            <div class="text-sm text-gray-500 dark:text-gray-400">
              {{ cat.count }} {{ cat.count === 1 ? 'item' : 'items' }}
            </div>
          </div>
          <div class="text-right shrink-0">
            <div class="font-semibold">{{ cat.formatted }}</div>
            <UIcon
              v-if="cat.id === 'flux'"
              :name="fluxExpanded ? 'i-heroicons-chevron-up' : 'i-heroicons-chevron-down'"
              class="w-4 h-4 text-gray-400 mt-1"
            />
          </div>
        </button>
      </div>
    </div>

    <!-- Section 3: FLUX Drill-down -->
    <div v-if="fluxExpanded" class="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
      <div class="p-4 border-b border-gray-200 dark:border-gray-700">
        <h3 class="font-semibold flex items-center gap-2">
          <UIcon name="i-heroicons-sparkles" class="w-5 h-5 text-purple-500" />
          AI / FLUX Storage Detail
        </h3>
        <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Total: {{ fluxStorage?.total_formatted || '—' }}
        </p>
      </div>

      <div v-if="fluxLoading" class="p-8 text-center">
        <UIcon name="i-heroicons-arrow-path" class="w-6 h-6 animate-spin text-gray-400" />
      </div>

      <div v-else class="divide-y divide-gray-200 dark:divide-gray-700">
        <div v-for="cat in fluxCategories" :key="cat.id">
          <button
            class="w-full p-4 flex items-center justify-between hover:bg-gray-50 dark:hover:bg-gray-700/50 transition-colors"
            :class="{ 'bg-gray-50 dark:bg-gray-700/50': selectedFluxCategory === cat.id }"
            :disabled="cat.readonly"
            @click="!cat.readonly && loadFluxFiles(cat.id)"
          >
            <div class="flex items-center gap-3">
              <div
                class="w-8 h-8 rounded-md flex items-center justify-center"
                :class="iconBgMap[cat.color] || 'bg-gray-100 dark:bg-gray-700 text-gray-500'"
              >
                <UIcon :name="cat.icon" class="w-4 h-4" />
              </div>
              <div class="text-left">
                <div class="font-medium text-sm">{{ cat.name }}</div>
                <div class="text-xs text-gray-500">{{ cat.count }} {{ cat.count === 1 ? 'item' : 'items' }}</div>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-sm font-semibold">{{ cat.formatted }}</span>
              <UIcon
                v-if="!cat.readonly"
                :name="selectedFluxCategory === cat.id ? 'i-heroicons-chevron-up' : 'i-heroicons-chevron-down'"
                class="w-4 h-4 text-gray-400"
              />
            </div>
          </button>

          <!-- FLUX file list -->
          <div v-if="selectedFluxCategory === cat.id" class="border-t border-gray-100 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/30">
            <div v-if="loadingFluxFiles" class="p-4 text-center">
              <UIcon name="i-heroicons-arrow-path" class="w-5 h-5 animate-spin text-gray-400" />
            </div>
            <div v-else-if="fluxFiles.length === 0" class="p-4 text-center text-sm text-gray-500">
              No items found
            </div>
            <div v-else class="divide-y divide-gray-100 dark:divide-gray-700">
              <div
                v-for="file in fluxFiles"
                :key="file.id"
                class="px-4 py-3 flex items-center justify-between hover:bg-gray-100 dark:hover:bg-gray-800/50"
              >
                <div class="flex items-center gap-3 min-w-0">
                  <UIcon :name="cat.icon" class="w-4 h-4 text-gray-400 shrink-0" />
                  <div class="min-w-0">
                    <div class="font-medium text-sm truncate">{{ file.name }}</div>
                    <div v-if="file.created_at" class="text-xs text-gray-500">{{ file.created_at }}</div>
                  </div>
                </div>
                <div class="flex items-center gap-3 shrink-0">
                  <span class="text-sm text-gray-500">{{ file.formatted }}</span>
                  <UButton
                    v-if="cat.id !== 'venv'"
                    variant="ghost"
                    color="error"
                    size="xs"
                    icon="i-heroicons-trash"
                    @click.stop="deleteFile(file)"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
