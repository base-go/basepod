<script setup lang="ts">
interface FluxModel {
  id: string
  name: string
  description: string
  size: string
  downloaded: boolean
  default_steps: number
  ram_required: number
}

interface FluxStatus {
  supported: boolean
  unsupported_reason?: string
  generating: boolean
  current_job?: FluxGeneration
  models_count: number
  queued_jobs: number
  processing_id?: string
  system_ram: number
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
  type?: string
  image_paths?: string[]
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
const selectedModel = ref('')
const selectedSize = ref('1024x1024')
const steps = ref(4)
const seed = ref(-1)

// Set default size based on system RAM
watch(status, (s) => {
  if (s?.system_ram && s.system_ram <= 16 && selectedSize.value === '1024x1024') {
    selectedSize.value = '512x512'
  }
}, { immediate: true })

// UI state
const generating = ref(false)
const currentJobId = ref<string | null>(null)
const downloadingModels = ref<Set<string>>(new Set())
const downloadProgress = ref<Map<string, DownloadProgress>>(new Map())
const selectedImage = ref<FluxGeneration | null>(null)
const showImageModal = ref(false)

// Tabs
const tabs = [
  { label: 'Generate', value: 'generate', icon: 'i-heroicons-sparkles' },
  { label: 'Edit', value: 'edit', icon: 'i-heroicons-pencil-square' },
  { label: 'Models', value: 'models', icon: 'i-heroicons-cpu-chip' },
  { label: 'Gallery', value: 'gallery', icon: 'i-heroicons-squares-2x2' }
]
const activeTab = ref('generate')

// Edit mode state
const editPrompt = ref('')
const editModel = ref('flux2-klein-9b')
const uploadedImages = ref<{ path: string, filename: string, preview: string }[]>([])
const uploading = ref(false)

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

// Set default selected model when data loads
watch(models, (newModels) => {
  if (newModels && newModels.length > 0 && !selectedModel.value) {
    const downloaded = newModels.find(m => m.downloaded)
    if (downloaded) {
      selectedModel.value = downloaded.id
      steps.value = downloaded.default_steps || 4
    }
  }
}, { immediate: true })

// Update steps when model changes
watch(selectedModel, (model) => {
  const m = models.value?.find(m => m.id === model)
  if (m) {
    steps.value = m.default_steps || 4
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

  if (!selectedModel.value) {
    toast.add({ title: 'Please select a model', color: 'warning' })
    return
  }

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
    generating.value = true
    toast.add({ title: 'Generation queued', color: 'info' })
    await refreshStatus()

    // Poll for completion
    pollJobStatus(job.id)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to start generation', description: err.data?.error, color: 'error' })
  }
}

// Upload image for editing
async function uploadImage(event: Event) {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  if (!file) return

  uploading.value = true
  const formData = new FormData()
  formData.append('image', file)

  try {
    const result = await $api<{ path: string, filename: string }>('/flux/upload', {
      method: 'POST',
      body: formData
    })

    // Create preview URL
    const preview = URL.createObjectURL(file)
    uploadedImages.value.push({
      path: result.path,
      filename: result.filename,
      preview
    })
    toast.add({ title: 'Image uploaded', color: 'success' })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to upload', description: err.data?.error, color: 'error' })
  } finally {
    uploading.value = false
    target.value = '' // Reset input
  }
}

// Remove uploaded image
function removeUploadedImage(index: number) {
  URL.revokeObjectURL(uploadedImages.value[index].preview)
  uploadedImages.value.splice(index, 1)
}

// Generate edited image
async function generateEdit() {
  if (!editPrompt.value.trim()) {
    toast.add({ title: 'Please enter a prompt', color: 'warning' })
    return
  }

  if (uploadedImages.value.length === 0) {
    toast.add({ title: 'Please upload at least one reference image', color: 'warning' })
    return
  }

  try {
    const job = await $api<FluxGeneration>('/flux/edit', {
      method: 'POST',
      body: {
        prompt: editPrompt.value,
        model: editModel.value,
        width: dimensions.value.width,
        height: dimensions.value.height,
        steps: steps.value,
        seed: seed.value,
        image_paths: uploadedImages.value.map(img => img.path)
      }
    })

    currentJobId.value = job.id
    generating.value = true
    toast.add({ title: 'Edit job queued', color: 'info' })
    await refreshStatus()

    // Poll for completion
    pollJobStatus(job.id)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to start edit', description: err.data?.error, color: 'error' })
  }
}

// Edit model options (only FLUX.2 Klein supports editing)
const editModelOptions = computed(() => {
  return models.value?.filter(m => m.id.startsWith('flux2-klein') && m.downloaded)
    .map(m => ({ label: m.name, value: m.id })) || []
})

const hasEditModel = computed(() => editModelOptions.value.length > 0)

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
        activeTab.value = 'gallery'
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
  link.href = gen.image_url
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
          <span v-if="status?.system_ram" class="ml-2 text-xs">({{ status.system_ram }}GB RAM)</span>
        </p>
      </div>
      <div class="flex items-center gap-3">
        <div v-if="status?.queued_jobs" class="flex items-center gap-2 px-3 py-2 bg-amber-100 dark:bg-amber-900/30 rounded-lg">
          <UIcon name="i-heroicons-queue-list" class="w-4 h-4 text-amber-600 dark:text-amber-400" />
          <span class="text-sm font-medium text-amber-700 dark:text-amber-300">
            {{ status.queued_jobs }} in queue
          </span>
        </div>
        <div v-if="status?.generating" class="flex items-center gap-2 px-3 py-2 bg-primary-100 dark:bg-primary-900/30 rounded-lg">
          <span class="w-2 h-2 bg-primary-500 rounded-full animate-pulse" />
          <span class="text-sm font-medium text-primary-700 dark:text-primary-300">
            Generating...
          </span>
        </div>
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
      <!-- Tabs -->
      <UTabs v-model="activeTab" :items="tabs" class="mb-6" />

      <!-- Generate Tab -->
      <div v-if="activeTab === 'generate'">
        <div v-if="!hasDownloadedModel" class="text-center py-12">
          <UIcon name="i-heroicons-cpu-chip" class="text-5xl text-gray-400 mb-4" />
          <h3 class="text-lg font-medium mb-2">No Models Downloaded</h3>
          <p class="text-gray-500 mb-4">Download a FLUX model to start generating images.</p>
          <UButton @click="activeTab = 'models'">
            <UIcon name="i-heroicons-arrow-down-tray" class="mr-2" />
            Download Models
          </UButton>
        </div>

        <div v-else class="space-y-6">
          <!-- Memory Warning for 16GB systems -->
          <div v-if="status?.system_ram && status.system_ram <= 16" class="p-3 bg-amber-50 dark:bg-amber-900/20 rounded-lg border border-amber-200 dark:border-amber-800">
            <div class="flex items-center gap-2 text-amber-700 dark:text-amber-300 text-sm">
              <UIcon name="i-heroicons-exclamation-triangle" class="w-5 h-5 flex-shrink-0" />
              <span>With {{ status.system_ram }}GB RAM, use 512x512 size for best results. Larger sizes may fail.</span>
            </div>
          </div>

          <!-- Prompt Input -->
          <div class="p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
            <UTextarea
              v-model="prompt"
              placeholder="Describe the image you want to create..."
              :rows="3"
              class="w-full mb-4"
              autofocus
            />

            <div class="flex flex-wrap gap-4 items-end">
              <div v-if="downloadedModels.length > 1" class="flex-1 min-w-[150px]">
                <label class="text-xs text-gray-500 mb-1 block">Model</label>
                <USelect
                  v-model="selectedModel"
                  :items="downloadedModels.map(m => ({ label: m.name, value: m.id }))"
                />
              </div>
              <div v-else-if="downloadedModels.length === 1" class="flex items-center gap-2">
                <span class="text-sm text-gray-500">Model:</span>
                <span class="font-medium">{{ downloadedModels[0].name }}</span>
              </div>

              <div class="w-[160px]">
                <label class="text-xs text-gray-500 mb-1 block">Size</label>
                <USelect
                  v-model="selectedSize"
                  :items="sizePresets"
                />
              </div>

              <div class="w-[100px]">
                <label class="text-xs text-gray-500 mb-1 block">Steps</label>
                <UInput v-model.number="steps" type="number" :min="1" :max="50" />
              </div>

              <div class="w-[120px]">
                <label class="text-xs text-gray-500 mb-1 block">Seed (-1 = random)</label>
                <UInput v-model.number="seed" type="number" />
              </div>

              <UButton
                color="primary"
                size="lg"
                :loading="generating"
                :disabled="!prompt.trim() || generating"
                @click="generate"
              >
                <UIcon name="i-heroicons-sparkles" class="mr-2" />
                Generate
              </UButton>
            </div>
          </div>

          <!-- Progress -->
          <div v-if="generating && status?.current_job" class="p-4 bg-primary-50 dark:bg-primary-900/20 rounded-lg border border-primary-200 dark:border-primary-800">
            <div class="flex items-center justify-between mb-2">
              <span class="font-medium text-primary-700 dark:text-primary-300">Generating image...</span>
              <span class="text-sm text-primary-600 dark:text-primary-400">
                Step {{ Math.round((status.current_job.progress || 0) / 100 * steps) }}/{{ steps }}
              </span>
            </div>
            <div class="h-3 bg-primary-200 dark:bg-primary-800 rounded-full overflow-hidden">
              <div
                class="h-full bg-primary-500 transition-all duration-300"
                :style="{ width: `${status.current_job.progress || 0}%` }"
              />
            </div>
          </div>

          <!-- Recent Generations Preview -->
          <div v-if="generations?.length">
            <div class="flex items-center justify-between mb-3">
              <h3 class="font-medium">Recent Generations</h3>
              <UButton variant="ghost" size="xs" @click="activeTab = 'gallery'">
                View All
                <UIcon name="i-heroicons-arrow-right" class="ml-1" />
              </UButton>
            </div>
            <div class="flex gap-3 overflow-x-auto pb-2">
              <div
                v-for="gen in generations.slice(0, 6)"
                :key="gen.id"
                class="flex-shrink-0 w-24 h-24 bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden cursor-pointer hover:ring-2 hover:ring-primary-500 transition-all"
                @click="viewImage(gen)"
              >
                <img
                  v-if="gen.status === 'completed' && gen.image_url"
                  :src="gen.image_url"
                  :alt="gen.prompt"
                  class="w-full h-full object-cover"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Edit Tab -->
      <div v-if="activeTab === 'edit'">
        <div v-if="!hasEditModel" class="text-center py-12">
          <UIcon name="i-heroicons-pencil-square" class="text-5xl text-gray-400 mb-4" />
          <h3 class="text-lg font-medium mb-2">No Edit Models Downloaded</h3>
          <p class="text-gray-500 mb-4">Download a FLUX.2 Klein model to use image editing features.</p>
          <UButton @click="activeTab = 'models'">
            <UIcon name="i-heroicons-arrow-down-tray" class="mr-2" />
            Download Models
          </UButton>
        </div>

        <div v-else class="space-y-6">
          <!-- Reference Images Upload -->
          <div class="p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
            <h3 class="font-medium mb-3">Reference Images</h3>
            <p class="text-sm text-gray-500 mb-4">
              Upload one or more reference images. The AI will combine or modify them based on your prompt.
            </p>

            <!-- Uploaded Images Preview -->
            <div v-if="uploadedImages.length" class="flex flex-wrap gap-3 mb-4">
              <div
                v-for="(img, index) in uploadedImages"
                :key="img.filename"
                class="relative group"
              >
                <img
                  :src="img.preview"
                  :alt="img.filename"
                  class="w-24 h-24 object-cover rounded-lg"
                />
                <button
                  class="absolute -top-2 -right-2 w-6 h-6 bg-red-500 text-white rounded-full opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center"
                  @click="removeUploadedImage(index)"
                >
                  <UIcon name="i-heroicons-x-mark" class="w-4 h-4" />
                </button>
              </div>
            </div>

            <!-- Upload Button -->
            <label class="inline-flex items-center gap-2 px-4 py-2 bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors">
              <UIcon name="i-heroicons-photo" class="w-5 h-5" />
              <span>{{ uploading ? 'Uploading...' : 'Add Image' }}</span>
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp"
                class="hidden"
                :disabled="uploading"
                @change="uploadImage"
              />
            </label>
          </div>

          <!-- Memory Warning for 16GB systems -->
          <div v-if="status?.system_ram && status.system_ram <= 16" class="p-3 bg-amber-50 dark:bg-amber-900/20 rounded-lg border border-amber-200 dark:border-amber-800">
            <div class="flex items-center gap-2 text-amber-700 dark:text-amber-300 text-sm">
              <UIcon name="i-heroicons-exclamation-triangle" class="w-5 h-5 flex-shrink-0" />
              <span>With {{ status.system_ram }}GB RAM, edit may fail on large images. Use smaller reference images and close other apps.</span>
            </div>
          </div>

          <!-- Edit Prompt -->
          <div class="p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
            <UTextarea
              v-model="editPrompt"
              placeholder="Describe what you want to do with the images (e.g., 'Make the woman wear the eyeglasses')..."
              :rows="3"
              class="w-full mb-4"
            />

            <div class="flex flex-wrap gap-4 items-end">
              <div v-if="editModelOptions.length > 1" class="flex-1 min-w-[150px]">
                <label class="text-xs text-gray-500 mb-1 block">Model</label>
                <USelect
                  v-model="editModel"
                  :items="editModelOptions"
                />
              </div>
              <div v-else-if="editModelOptions.length === 1" class="flex items-center gap-2">
                <span class="text-sm text-gray-500">Model:</span>
                <span class="font-medium">{{ editModelOptions[0].label }}</span>
              </div>

              <div class="w-[100px]">
                <label class="text-xs text-gray-500 mb-1 block">Steps</label>
                <UInput v-model.number="steps" type="number" :min="1" :max="50" />
              </div>

              <div class="w-[120px]">
                <label class="text-xs text-gray-500 mb-1 block">Seed (-1 = random)</label>
                <UInput v-model.number="seed" type="number" />
              </div>

              <UButton
                color="primary"
                size="lg"
                :loading="generating"
                :disabled="!editPrompt.trim() || uploadedImages.length === 0 || generating"
                @click="generateEdit"
              >
                <UIcon name="i-heroicons-pencil-square" class="mr-2" />
                Edit Image
              </UButton>
            </div>
          </div>

          <!-- Progress -->
          <div v-if="generating && status?.current_job" class="p-4 bg-primary-50 dark:bg-primary-900/20 rounded-lg border border-primary-200 dark:border-primary-800">
            <div class="flex items-center justify-between mb-2">
              <span class="font-medium text-primary-700 dark:text-primary-300">Processing edit...</span>
              <span class="text-sm text-primary-600 dark:text-primary-400">
                Step {{ Math.round((status.current_job.progress || 0) / 100 * steps) }}/{{ steps }}
              </span>
            </div>
            <div class="h-3 bg-primary-200 dark:bg-primary-800 rounded-full overflow-hidden">
              <div
                class="h-full bg-primary-500 transition-all duration-300"
                :style="{ width: `${status.current_job.progress || 0}%` }"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Models Tab -->
      <div v-if="activeTab === 'models'" class="space-y-4">
        <div
          v-for="model in sortedModels"
          :key="model.id"
          class="p-4 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
        >
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-4 flex-1">
              <div class="w-12 h-12 bg-primary-100 dark:bg-primary-900/50 rounded-lg flex items-center justify-center">
                <UIcon name="i-heroicons-cpu-chip" class="w-6 h-6 text-primary-500" />
              </div>
              <div>
                <div class="font-medium flex items-center gap-2">
                  {{ model.name }}
                  <UBadge v-if="model.downloaded" color="success" variant="soft" size="xs">
                    Compatible
                  </UBadge>
                  <UBadge v-else color="amber" variant="soft" size="xs">
                    {{ model.ram_required }}GB+ RAM needed
                  </UBadge>
                </div>
                <div class="text-sm text-gray-500">{{ model.description }}</div>
                <div class="text-xs text-gray-400 mt-1">{{ model.size }} · {{ model.default_steps }} steps · Requires {{ model.ram_required }}GB RAM</div>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <!-- If compatible (enough RAM) -->
              <template v-if="model.downloaded">
                <UButton variant="soft" @click="activeTab = 'generate'; selectedModel = model.id">
                  Use
                </UButton>
              </template>

              <!-- Not enough RAM -->
              <template v-else>
                <UBadge color="amber" variant="soft">
                  Needs {{ model.ram_required }}GB RAM
                </UBadge>
              </template>
            </div>
          </div>
        </div>
      </div>

      <!-- Gallery Tab -->
      <div v-if="activeTab === 'gallery'">
        <div v-if="!generations?.length" class="text-center py-12">
          <UIcon name="i-heroicons-photo" class="text-5xl text-gray-400 mb-4" />
          <h3 class="text-lg font-medium mb-2">No Images Yet</h3>
          <p class="text-gray-500 mb-4">Generate your first image to see it here.</p>
          <UButton @click="activeTab = 'generate'">
            <UIcon name="i-heroicons-sparkles" class="mr-2" />
            Generate Image
          </UButton>
        </div>

        <div v-else class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
          <div
            v-for="gen in generations"
            :key="gen.id"
            class="group relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden cursor-pointer"
            @click="viewImage(gen)"
          >
            <img
              v-if="gen.status === 'completed' && gen.image_url"
              :src="gen.image_url"
              :alt="gen.prompt"
              class="w-full h-full object-cover"
            />
            <div
              v-else-if="gen.status === 'generating'"
              class="w-full h-full flex flex-col items-center justify-center gap-2"
            >
              <UIcon name="i-heroicons-arrow-path" class="text-2xl animate-spin text-primary-400" />
              <span class="text-xs text-gray-500">Generating...</span>
            </div>
            <div
              v-else-if="gen.status === 'queued'"
              class="w-full h-full flex flex-col items-center justify-center gap-2"
            >
              <UIcon name="i-heroicons-queue-list" class="text-2xl text-amber-400" />
              <span class="text-xs text-gray-500">Queued</span>
            </div>
            <div
              v-else-if="gen.status === 'pending'"
              class="w-full h-full flex flex-col items-center justify-center gap-2"
            >
              <UIcon name="i-heroicons-clock" class="text-2xl text-gray-400" />
              <span class="text-xs text-gray-500">Pending...</span>
            </div>
            <div
              v-else-if="gen.status === 'failed'"
              class="w-full h-full flex flex-col items-center justify-center gap-2"
            >
              <UIcon name="i-heroicons-exclamation-circle" class="text-2xl text-red-400" />
              <span class="text-xs text-red-500">Failed</span>
            </div>

            <!-- Hover overlay -->
            <div class="absolute inset-0 bg-black/50 opacity-0 group-hover:opacity-100 transition-opacity flex items-end p-3">
              <p class="text-white text-sm line-clamp-2">{{ gen.prompt }}</p>
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
              :src="selectedImage.image_url"
              :alt="selectedImage.prompt"
              class="w-full rounded-lg"
            />

            <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <div class="text-xs text-gray-500 mb-1">Prompt</div>
              <p class="text-sm">{{ selectedImage.prompt }}</p>
            </div>

            <div class="grid grid-cols-2 gap-4">
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Type</div>
                <div class="font-medium flex items-center gap-2">
                  <UBadge v-if="selectedImage.type === 'edit'" color="purple" variant="soft" size="xs">
                    Edit
                  </UBadge>
                  <UBadge v-else color="primary" variant="soft" size="xs">
                    Generate
                  </UBadge>
                </div>
              </div>
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
              <div v-if="selectedImage.image_paths?.length" class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg col-span-2">
                <div class="text-xs text-gray-500 mb-1">Reference Images</div>
                <div class="font-medium">{{ selectedImage.image_paths.length }} image(s)</div>
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
