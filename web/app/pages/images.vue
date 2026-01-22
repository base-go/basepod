<script setup lang="ts">
interface FluxModel {
  id: string
  name: string
  description: string
  size: string
  downloaded: boolean
  default_steps: number
}

interface FluxStatus {
  supported: boolean
  unsupported_reason?: string
  generating: boolean
  current_job?: FluxGeneration
  models_count: number
}

interface FluxGeneration {
  id: string
  prompt: string
  model: string
  width: number
  height: number
  steps: number
  seed: number
  status: string
  progress?: number
  image_url?: string
  error?: string
  created_at: string
}

interface DownloadProgress {
  model_id: string
  status: string
  progress: number
  message: string
  bytes_done: number
  bytes_total: number
  speed: number
  eta: number
}

definePageMeta({
  title: 'Images'
})

const toast = useToast()

// Fetch status and models
const { data: status, refresh: refreshStatus } = await useApiFetch<FluxStatus>('/flux/status')
const { data: models, refresh: refreshModels } = await useApiFetch<FluxModel[]>('/flux/models')
const { data: generations, refresh: refreshGenerations } = await useApiFetch<FluxGeneration[]>('/flux/generations')

// Form state
const prompt = ref('')
const selectedModel = ref('schnell')
const selectedSize = ref('1024x1024')
const steps = ref(4)
const seed = ref(-1)

// UI state
const generating = ref(false)
const currentJobId = ref<string | null>(null)
const downloadingModels = ref<Set<string>>(new Set())
const downloadProgress = ref<Map<string, DownloadProgress>>(new Map())
const selectedImage = ref<FluxGeneration | null>(null)
const showImageModal = ref(false)

// Size presets
const sizePresets = [
  { label: '512x512', value: '512x512' },
  { label: '768x768', value: '768x768' },
  { label: '1024x1024', value: '1024x1024' },
  { label: '1024x768 (Landscape)', value: '1024x768' },
  { label: '768x1024 (Portrait)', value: '768x1024' },
]

// Compute dimensions from size
const dimensions = computed(() => {
  const [w, h] = selectedSize.value.split('x').map(Number)
  return { width: w, height: h }
})

// Update steps when model changes
watch(selectedModel, (model) => {
  const m = models.value?.find(m => m.id === model)
  if (m) {
    steps.value = m.default_steps || (model === 'schnell' ? 4 : 20)
  }
})

// Downloaded models
const downloadedModels = computed(() => {
  return models.value?.filter(m => m.downloaded) || []
})

// Check if any model is downloaded
const hasDownloadedModel = computed(() => {
  return downloadedModels.value.length > 0
})

// Sorted models - downloaded first
const sortedModels = computed(() => {
  if (!models.value) return []
  return [...models.value].sort((a, b) => {
    if (a.downloaded && !b.downloaded) return -1
    if (!a.downloaded && b.downloaded) return 1
    return a.name.localeCompare(b.name)
  })
})

// Generate image
async function generate() {
  if (!prompt.value.trim()) {
    toast.add({ title: 'Please enter a prompt', color: 'warning' })
    return
  }

  generating.value = true
  currentJobId.value = null

  try {
    const job = await $api<FluxGeneration>('/flux/generate', {
      method: 'POST',
      body: {
        prompt: prompt.value,
        model: selectedModel.value,
        width: dimensions.value.width,
        height: dimensions.value.height,
        steps: steps.value,
        seed: seed.value,
      }
    })

    currentJobId.value = job.id
    toast.add({ title: 'Generation started', color: 'info' })

    // Poll for completion
    pollJobStatus(job.id)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to start generation', description: err.data?.error, color: 'error' })
    generating.value = false
  }
}

// Poll job status
async function pollJobStatus(jobId: string) {
  const maxAttempts = 300 // 5 minutes max
  let attempts = 0

  while (attempts < maxAttempts) {
    try {
      const job = await $api<FluxGeneration>(`/flux/jobs/${jobId}`)

      if (job.status === 'completed') {
        generating.value = false
        currentJobId.value = null
        toast.add({ title: 'Image generated!', color: 'success' })
        refreshGenerations()
        return
      }

      if (job.status === 'failed') {
        generating.value = false
        currentJobId.value = null
        toast.add({ title: 'Generation failed', description: job.error, color: 'error' })
        return
      }

      // Update status display
      await refreshStatus()
    } catch {
      // Ignore polling errors
    }

    await new Promise(resolve => setTimeout(resolve, 1000))
    attempts++
  }

  generating.value = false
  toast.add({ title: 'Generation timed out', color: 'error' })
}

// Download model
async function downloadModel(modelId: string) {
  downloadingModels.value.add(modelId)

  try {
    await $api(`/flux/models/${modelId}`, { method: 'POST' })
    toast.add({ title: `Downloading ${modelId}...`, color: 'info' })

    // Poll download progress
    pollDownloadProgress(modelId)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to start download', description: err.data?.error, color: 'error' })
    downloadingModels.value.delete(modelId)
  }
}

// Poll download progress
async function pollDownloadProgress(modelId: string) {
  while (downloadingModels.value.has(modelId)) {
    try {
      const progress = await $api<DownloadProgress>(`/flux/models/${modelId}/progress`)
      downloadProgress.value.set(modelId, progress)

      if (progress.status === 'completed') {
        downloadingModels.value.delete(modelId)
        downloadProgress.value.delete(modelId)
        toast.add({ title: 'Model downloaded!', color: 'success' })
        await refreshModels()
        await refreshStatus()
        return
      }

      if (progress.status === 'failed') {
        downloadingModels.value.delete(modelId)
        downloadProgress.value.delete(modelId)
        toast.add({ title: 'Download failed', description: progress.message, color: 'error' })
        return
      }
    } catch {
      // Ignore polling errors
    }

    await new Promise(resolve => setTimeout(resolve, 1000))
  }
}

// Delete model
async function deleteModel(modelId: string) {
  if (!confirm(`Delete ${modelId} model? This cannot be undone.`)) return

  try {
    await $api(`/flux/models/${modelId}`, { method: 'DELETE' })
    toast.add({ title: 'Model deleted', color: 'success' })
    await refreshModels()
    await refreshStatus()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to delete model', description: err.data?.error, color: 'error' })
  }
}

// View image
function viewImage(gen: FluxGeneration) {
  selectedImage.value = gen
  showImageModal.value = true
}

// Delete generation
async function deleteGeneration(id: string) {
  if (!confirm('Delete this image?')) return

  try {
    await $api(`/flux/generations/${id}`, { method: 'DELETE' })
    toast.add({ title: 'Image deleted', color: 'success' })
    showImageModal.value = false
    refreshGenerations()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to delete', description: err.data?.error, color: 'error' })
  }
}

// Download image
function downloadImage(gen: FluxGeneration) {
  if (!gen.image_url) return
  const link = document.createElement('a')
  link.href = `/api${gen.image_url}`
  link.download = `${gen.id}.png`
  link.click()
}

// Format date
function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleString()
}

// Format bytes to human readable
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

// Format seconds to human readable ETA
function formatETA(seconds: number): string {
  if (seconds <= 0) return ''
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

// Auto-refresh status
let refreshTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  refreshTimer = setInterval(() => {
    if (generating.value) {
      refreshStatus()
    }
  }, 2000)
})
onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<template>
  <div>
    <!-- Header with Status -->
    <div class="mb-6 flex items-center justify-between">
      <div>
        <h2 class="text-xl font-semibold">Image Generation</h2>
        <p class="text-gray-500 dark:text-gray-400">
          Generate images with FLUX on Apple Silicon
        </p>
      </div>
      <div v-if="generating" class="flex items-center gap-2 px-3 py-2 bg-primary-100 dark:bg-primary-900/30 rounded-lg">
        <span class="w-2 h-2 bg-primary-500 rounded-full animate-pulse" />
        <span class="text-sm font-medium text-primary-700 dark:text-primary-300">
          Generating...
        </span>
      </div>
    </div>

    <!-- Not Supported Warning -->
    <div v-if="status && !status.supported" class="p-6 bg-amber-50 dark:bg-amber-900/20 rounded-lg mb-6 border border-amber-200 dark:border-amber-800">
      <div class="flex items-start gap-4">
        <UIcon name="i-heroicons-exclamation-triangle" class="w-8 h-8 text-amber-500 flex-shrink-0" />
        <div>
          <h3 class="font-semibold text-amber-800 dark:text-amber-200 text-lg">Image Generation Not Available</h3>
          <p class="text-amber-700 dark:text-amber-300 mt-1">
            {{ status.unsupported_reason || 'FLUX requires macOS with Apple Silicon (M series) and mflux installed.' }}
          </p>
          <div class="mt-4 p-3 bg-amber-100 dark:bg-amber-800/50 rounded text-sm">
            <p class="text-amber-800 dark:text-amber-200">
              <strong>To use image generation:</strong> Run Basepod on a Mac with Apple Silicon and install mflux via pip.
            </p>
          </div>
        </div>
      </div>
    </div>

    <template v-else>
      <!-- Generation Form (only if model downloaded) -->
      <div v-if="hasDownloadedModel" class="mb-6 p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
        <div class="flex items-center gap-2 mb-3">
          <UIcon name="i-heroicons-sparkles" class="w-5 h-5 text-primary-500" />
          <h3 class="font-medium">Generate Image</h3>
        </div>

        <form class="space-y-4" @submit.prevent="generate">
          <UTextarea
            v-model="prompt"
            placeholder="Describe the image you want to create..."
            :rows="2"
            class="w-full"
          />

          <div class="flex flex-wrap gap-3 items-end">
            <div class="flex-1 min-w-[120px]">
              <label class="text-xs text-gray-500 mb-1 block">Model</label>
              <USelect
                v-model="selectedModel"
                :items="downloadedModels.map(m => ({ label: m.name, value: m.id }))"
                size="sm"
              />
            </div>

            <div class="w-[140px]">
              <label class="text-xs text-gray-500 mb-1 block">Size</label>
              <USelect
                v-model="selectedSize"
                :items="sizePresets"
                size="sm"
              />
            </div>

            <div class="w-[80px]">
              <label class="text-xs text-gray-500 mb-1 block">Steps</label>
              <UInput v-model.number="steps" type="number" :min="1" :max="50" size="sm" />
            </div>

            <div class="w-[100px]">
              <label class="text-xs text-gray-500 mb-1 block">Seed (-1 = random)</label>
              <UInput v-model.number="seed" type="number" size="sm" />
            </div>

            <UButton
              type="submit"
              color="primary"
              :loading="generating"
              :disabled="!prompt.trim() || generating"
            >
              Generate
            </UButton>
          </div>

          <!-- Progress -->
          <div v-if="generating && status?.current_job" class="mt-3">
            <div class="flex items-center justify-between text-xs text-gray-500 mb-1">
              <span>{{ Math.round(status.current_job.progress || 0) }}%</span>
              <span>Step {{ Math.round((status.current_job.progress || 0) / 100 * steps) }}/{{ steps }}</span>
            </div>
            <div class="h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
              <div
                class="h-full bg-primary-500 transition-all duration-300"
                :style="{ width: `${status.current_job.progress || 0}%` }"
              />
            </div>
          </div>
        </form>
      </div>

      <!-- Models List -->
      <div class="space-y-8">
        <div>
          <div class="mb-4">
            <div class="flex items-center gap-2 mb-1">
              <UIcon name="i-heroicons-photo" class="w-5 h-5 text-gray-600 dark:text-gray-400" />
              <h3 class="text-lg font-semibold">FLUX Models</h3>
            </div>
            <p class="text-sm text-gray-500">Download and manage image generation models</p>
          </div>

          <div class="space-y-3">
            <div
              v-for="model in sortedModels"
              :key="model.id"
              class="p-4 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
            >
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-4 flex-1">
                  <div class="w-10 h-10 bg-primary-100 dark:bg-primary-900/50 rounded-lg flex items-center justify-center">
                    <UIcon name="i-heroicons-photo" class="w-5 h-5 text-primary-500" />
                  </div>
                  <div>
                    <div class="font-medium flex items-center gap-2">
                      {{ model.name }}
                      <span v-if="model.downloaded" class="text-xs bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 px-2 py-0.5 rounded">
                        Downloaded
                      </span>
                    </div>
                    <div class="text-sm text-gray-500">{{ model.description }}</div>
                  </div>
                </div>
                <div class="flex items-center gap-3">
                  <span class="text-sm text-gray-400">{{ model.size }}</span>

                  <!-- If downloading - show progress -->
                  <div v-if="downloadingModels.has(model.id)" class="flex items-center gap-3 min-w-[200px]">
                    <div class="flex-1">
                      <div class="flex items-center justify-between text-xs text-gray-500 mb-1">
                        <span v-if="downloadProgress.get(model.id)?.progress !== undefined">
                          {{ Math.round(downloadProgress.get(model.id)!.progress) }}%
                        </span>
                        <span v-else>Starting...</span>
                        <span v-if="downloadProgress.get(model.id)?.eta">
                          ETA: {{ formatETA(downloadProgress.get(model.id)!.eta) }}
                        </span>
                      </div>
                      <div class="h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                        <div
                          class="h-full bg-primary-500 transition-all duration-300"
                          :style="{ width: `${downloadProgress.get(model.id)?.progress || 0}%` }"
                        />
                      </div>
                      <div class="flex items-center justify-between text-xs text-gray-400 mt-1">
                        <span v-if="downloadProgress.get(model.id)?.bytes_done">
                          {{ formatBytes(downloadProgress.get(model.id)!.bytes_done) }} / {{ formatBytes(downloadProgress.get(model.id)!.bytes_total) }}
                        </span>
                        <span v-else>{{ downloadProgress.get(model.id)?.message || 'Downloading...' }}</span>
                        <span v-if="downloadProgress.get(model.id)?.speed">
                          {{ formatBytes(downloadProgress.get(model.id)!.speed) }}/s
                        </span>
                      </div>
                    </div>
                  </div>

                  <!-- If downloaded -->
                  <UButton
                    v-else-if="model.downloaded"
                    size="sm"
                    variant="soft"
                    color="error"
                    @click="deleteModel(model.id)"
                  >
                    Delete
                  </UButton>

                  <!-- Not downloaded -->
                  <UButton
                    v-else
                    size="sm"
                    variant="soft"
                    @click="downloadModel(model.id)"
                  >
                    Download
                  </UButton>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Gallery -->
      <div v-if="generations?.length" class="mt-8">
        <div class="flex items-center justify-between mb-4">
          <div class="flex items-center gap-2">
            <UIcon name="i-heroicons-squares-2x2" class="w-5 h-5 text-gray-600 dark:text-gray-400" />
            <h3 class="text-lg font-semibold">Gallery</h3>
          </div>
          <UButton variant="ghost" size="xs" @click="refreshGenerations">
            <UIcon name="i-heroicons-arrow-path" />
          </UButton>
        </div>

        <div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
          <div
            v-for="gen in generations"
            :key="gen.id"
            class="group relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden cursor-pointer"
            @click="viewImage(gen)"
          >
            <img
              v-if="gen.status === 'completed' && gen.image_url"
              :src="`/api${gen.image_url}`"
              :alt="gen.prompt"
              class="w-full h-full object-cover"
            />
            <div
              v-else-if="gen.status === 'generating'"
              class="w-full h-full flex items-center justify-center"
            >
              <UIcon name="i-heroicons-arrow-path" class="text-2xl animate-spin text-gray-400" />
            </div>
            <div
              v-else-if="gen.status === 'failed'"
              class="w-full h-full flex items-center justify-center"
            >
              <UIcon name="i-heroicons-exclamation-circle" class="text-2xl text-red-400" />
            </div>

            <!-- Hover overlay -->
            <div class="absolute inset-0 bg-black/50 opacity-0 group-hover:opacity-100 transition-opacity flex items-end p-2">
              <p class="text-white text-xs line-clamp-2">{{ gen.prompt }}</p>
            </div>
          </div>
        </div>
      </div>
    </template>

    <!-- Image Detail Modal -->
    <UModal v-model:open="showImageModal">
      <template v-if="selectedImage" #content>
        <div class="p-6">
          <div class="flex items-start justify-between mb-4">
            <h3 class="text-lg font-semibold">Image Details</h3>
            <UButton variant="ghost" size="sm" icon="i-heroicons-x-mark" @click="showImageModal = false" />
          </div>

          <div class="space-y-4">
            <img
              v-if="selectedImage.image_url"
              :src="`/api${selectedImage.image_url}`"
              :alt="selectedImage.prompt"
              class="w-full rounded-lg"
            />

            <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <div class="text-xs text-gray-500 mb-1">Prompt</div>
              <p class="text-sm">{{ selectedImage.prompt }}</p>
            </div>

            <div class="grid grid-cols-2 gap-4">
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Model</div>
                <div class="font-medium">{{ selectedImage.model }}</div>
              </div>
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Size</div>
                <div class="font-medium">{{ selectedImage.width }}x{{ selectedImage.height }}</div>
              </div>
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Steps</div>
                <div class="font-medium">{{ selectedImage.steps }}</div>
              </div>
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Seed</div>
                <div class="font-medium">{{ selectedImage.seed }}</div>
              </div>
            </div>

            <div class="text-xs text-gray-500">
              Created {{ formatDate(selectedImage.created_at) }}
            </div>

            <div class="flex gap-3 pt-2">
              <UButton class="flex-1" @click="downloadImage(selectedImage)">
                <UIcon name="i-heroicons-arrow-down-tray" class="mr-1" />
                Download
              </UButton>
              <UButton variant="soft" color="error" @click="deleteGeneration(selectedImage.id)">
                <UIcon name="i-heroicons-trash" class="mr-1" />
                Delete
              </UButton>
            </div>
          </div>
        </div>
      </template>
    </UModal>
  </div>
</template>
