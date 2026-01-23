<script setup lang="ts">
interface StorageItem {
  path: string
  size: number
  formatted: string
  count: number
}

interface StorageInfo {
  models: StorageItem
  generations: StorageItem
  uploads: StorageItem
  venv: StorageItem
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

// Fetch storage info
const { data: storage, refresh: refreshStorage } = await useApiFetch<StorageInfo>('/flux/storage')

// Selected category for detail view
const selectedCategory = ref<string | null>(null)
const categoryFiles = ref<StorageFile[]>([])
const loadingFiles = ref(false)

// Load files for a category
async function loadCategoryFiles(category: string) {
  if (selectedCategory.value === category) {
    selectedCategory.value = null
    categoryFiles.value = []
    return
  }

  loadingFiles.value = true
  selectedCategory.value = category
  try {
    categoryFiles.value = await $api<StorageFile[]>(`/flux/storage/${category}`)
  } catch {
    categoryFiles.value = []
  } finally {
    loadingFiles.value = false
  }
}

// Delete a file
async function deleteFile(file: StorageFile) {
  if (!confirm(`Delete ${file.name}? This cannot be undone.`)) return

  try {
    if (file.type === 'models') {
      await $api(`/flux/models/${file.id}`, { method: 'DELETE' })
    } else if (file.type === 'generations') {
      await $api(`/flux/generations/${file.id}`, { method: 'DELETE' })
    }
    toast.add({ title: 'Deleted successfully', color: 'success' })
    await refreshStorage()
    await loadCategoryFiles(file.type)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to delete', description: err.data?.error, color: 'error' })
  }
}

// Storage categories
const categories = computed(() => {
  if (!storage.value) return []
  return [
    {
      id: 'models',
      name: 'Models',
      icon: 'i-heroicons-cpu-chip',
      ...storage.value.models,
      color: 'primary'
    },
    {
      id: 'generations',
      name: 'Generated Images',
      icon: 'i-heroicons-photo',
      ...storage.value.generations,
      color: 'green'
    },
    {
      id: 'uploads',
      name: 'Uploads',
      icon: 'i-heroicons-arrow-up-tray',
      ...storage.value.uploads,
      color: 'amber'
    },
    {
      id: 'venv',
      name: 'Python Environment',
      icon: 'i-heroicons-code-bracket',
      ...storage.value.venv,
      color: 'gray',
      readonly: true
    }
  ]
})

// Calculate percentage
function getPercentage(size: number): number {
  if (!storage.value?.total || storage.value.total === 0) return 0
  return Math.round((size / storage.value.total) * 100)
}
</script>

<template>
  <div>
    <!-- Header -->
    <div class="mb-6">
      <h2 class="text-xl font-semibold">Storage</h2>
      <p class="text-gray-500 dark:text-gray-400">
        Manage disk space used by Basepod AI
      </p>
    </div>

    <!-- Total Usage -->
    <div class="mb-6 p-4 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700">
      <div class="flex items-center justify-between mb-3">
        <span class="text-sm text-gray-500">Total Storage Used</span>
        <span class="text-2xl font-bold">{{ storage?.total_formatted || '0 B' }}</span>
      </div>
      <div class="h-4 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden flex">
        <div
          v-for="cat in categories"
          :key="cat.id"
          class="h-full transition-all"
          :class="{
            'bg-primary-500': cat.color === 'primary',
            'bg-green-500': cat.color === 'green',
            'bg-amber-500': cat.color === 'amber',
            'bg-gray-500': cat.color === 'gray'
          }"
          :style="{ width: `${getPercentage(cat.size)}%` }"
          :title="`${cat.name}: ${cat.formatted}`"
        />
      </div>
      <div class="flex flex-wrap gap-4 mt-3">
        <div v-for="cat in categories" :key="cat.id" class="flex items-center gap-2 text-sm">
          <span
            class="w-3 h-3 rounded-full"
            :class="{
              'bg-primary-500': cat.color === 'primary',
              'bg-green-500': cat.color === 'green',
              'bg-amber-500': cat.color === 'amber',
              'bg-gray-500': cat.color === 'gray'
            }"
          />
          <span class="text-gray-600 dark:text-gray-400">{{ cat.name }}</span>
          <span class="font-medium">{{ cat.formatted }}</span>
        </div>
      </div>
    </div>

    <!-- Categories -->
    <div class="space-y-4">
      <div
        v-for="cat in categories"
        :key="cat.id"
        class="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden"
      >
        <button
          class="w-full p-4 flex items-center justify-between hover:bg-gray-50 dark:hover:bg-gray-700/50 transition-colors"
          :class="{ 'bg-gray-50 dark:bg-gray-700/50': selectedCategory === cat.id }"
          @click="!cat.readonly && loadCategoryFiles(cat.id)"
          :disabled="cat.readonly"
        >
          <div class="flex items-center gap-3">
            <div
              class="w-10 h-10 rounded-lg flex items-center justify-center"
              :class="{
                'bg-primary-100 dark:bg-primary-900/50 text-primary-500': cat.color === 'primary',
                'bg-green-100 dark:bg-green-900/50 text-green-500': cat.color === 'green',
                'bg-amber-100 dark:bg-amber-900/50 text-amber-500': cat.color === 'amber',
                'bg-gray-100 dark:bg-gray-700 text-gray-500': cat.color === 'gray'
              }"
            >
              <UIcon :name="cat.icon" class="w-5 h-5" />
            </div>
            <div class="text-left">
              <div class="font-medium">{{ cat.name }}</div>
              <div class="text-sm text-gray-500">{{ cat.count }} {{ cat.count === 1 ? 'item' : 'items' }}</div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <span class="font-semibold">{{ cat.formatted }}</span>
            <UIcon
              v-if="!cat.readonly"
              :name="selectedCategory === cat.id ? 'i-heroicons-chevron-up' : 'i-heroicons-chevron-down'"
              class="w-5 h-5 text-gray-400"
            />
          </div>
        </button>

        <!-- Expanded file list -->
        <div v-if="selectedCategory === cat.id" class="border-t border-gray-200 dark:border-gray-700">
          <div v-if="loadingFiles" class="p-4 text-center">
            <UIcon name="i-heroicons-arrow-path" class="w-5 h-5 animate-spin text-gray-400" />
          </div>
          <div v-else-if="categoryFiles.length === 0" class="p-4 text-center text-gray-500">
            No items found
          </div>
          <div v-else class="divide-y divide-gray-200 dark:divide-gray-700">
            <div
              v-for="file in categoryFiles"
              :key="file.id"
              class="p-4 flex items-center justify-between hover:bg-gray-50 dark:hover:bg-gray-700/50"
            >
              <div class="flex items-center gap-3">
                <UIcon
                  :name="cat.icon"
                  class="w-5 h-5 text-gray-400"
                />
                <div>
                  <div class="font-medium">{{ file.name }}</div>
                  <div v-if="file.created_at" class="text-xs text-gray-500">{{ file.created_at }}</div>
                </div>
              </div>
              <div class="flex items-center gap-4">
                <span class="text-sm text-gray-500">{{ file.formatted }}</span>
                <UButton
                  v-if="cat.id !== 'venv'"
                  variant="ghost"
                  color="error"
                  size="sm"
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
</template>
