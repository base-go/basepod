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
}

definePageMeta({
  title: 'Images'
})

const toast = useToast()

// Fetch status and models
const { data: status, refresh: refreshStatus } = await useApiFetch<FluxStatus>('/flux/status')
const { data: models } = await useApiFetch<FluxModel[]>('/flux/models')
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
const downloadingModel = ref<string | null>(null)
const downloadProgress = ref<DownloadProgress | null>(null)
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
  downloadingModel.value = modelId

  try {
    await $api(`/flux/models/${modelId}`, { method: 'POST' })
    toast.add({ title: `Downloading ${modelId}...`, color: 'info' })

    // Poll download progress
    pollDownloadProgress(modelId)
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to start download', description: err.data?.error, color: 'error' })
    downloadingModel.value = null
  }
}

// Poll download progress
async function pollDownloadProgress(modelId: string) {
  while (downloadingModel.value === modelId) {
    try {
      const progress = await $api<DownloadProgress>(`/flux/models/${modelId}/progress`)
      downloadProgress.value = progress

      if (progress.status === 'completed') {
        downloadingModel.value = null
        downloadProgress.value = null
        toast.add({ title: 'Model downloaded!', color: 'success' })
        // Refresh models list
        await refreshStatus()
        return
      }

      if (progress.status === 'failed') {
        downloadingModel.value = null
        downloadProgress.value = null
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
</script>

<template>
  <div>
    <!-- Unsupported Message -->
    <UAlert
      v-if="status && !status.supported"
      color="warning"
      variant="soft"
      icon="i-heroicons-exclamation-triangle"
      title="FLUX Not Supported"
      :description="status.unsupported_reason || 'FLUX requires macOS with Apple Silicon (M series)'"
      class="mb-6"
    />

    <div v-else class="max-w-6xl space-y-6">
      <!-- Generation Form -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Generate Image</h3>
            <UBadge v-if="generating" color="primary" variant="soft">
              <span class="flex items-center gap-1">
                <UIcon name="i-heroicons-arrow-path" class="animate-spin" />
                Generating...
              </span>
            </UBadge>
          </div>
        </template>

        <!-- No Model Downloaded -->
        <div v-if="!hasDownloadedModel" class="text-center py-8">
          <UIcon name="i-heroicons-photo" class="text-4xl text-gray-400 mb-4" />
          <h3 class="text-lg font-medium mb-2">Download a Model First</h3>
          <p class="text-gray-500 mb-4">To generate images, you need to download a FLUX model.</p>

          <div class="flex flex-col gap-3 max-w-md mx-auto">
            <div
              v-for="model in models"
              :key="model.id"
              class="flex items-center justify-between p-3 border rounded-lg"
            >
              <div>
                <p class="font-medium">{{ model.name }}</p>
                <p class="text-sm text-gray-500">{{ model.description }}</p>
                <p class="text-xs text-gray-400">{{ model.size }}</p>
              </div>
              <div>
                <UButton
                  v-if="downloadingModel === model.id"
                  :loading="true"
                  disabled
                  size="sm"
                >
                  {{ downloadProgress?.progress?.toFixed(0) || 0 }}%
                </UButton>
                <UButton
                  v-else
                  color="primary"
                  size="sm"
                  @click="downloadModel(model.id)"
                >
                  Download
                </UButton>
              </div>
            </div>
          </div>
        </div>

        <!-- Generation Form -->
        <form v-else class="space-y-4" @submit.prevent="generate">
          <UFormField label="Prompt">
            <UTextarea
              v-model="prompt"
              placeholder="Describe the image you want to create..."
              :rows="3"
              autofocus
            />
          </UFormField>

          <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
            <UFormField label="Model">
              <USelect
                v-model="selectedModel"
                :items="downloadedModels.map(m => ({ label: m.name, value: m.id }))"
              />
            </UFormField>

            <UFormField label="Size">
              <USelect
                v-model="selectedSize"
                :items="sizePresets"
              />
            </UFormField>

            <UFormField label="Steps">
              <UInput v-model.number="steps" type="number" :min="1" :max="50" />
            </UFormField>

            <UFormField label="Seed" help="-1 for random">
              <UInput v-model.number="seed" type="number" />
            </UFormField>
          </div>

          <div class="flex items-center justify-between">
            <div class="text-sm text-gray-500">
              {{ dimensions.width }}x{{ dimensions.height }} · {{ steps }} steps
            </div>
            <UButton
              type="submit"
              color="primary"
              :loading="generating"
              :disabled="!prompt.trim() || generating"
            >
              <UIcon name="i-heroicons-sparkles" class="mr-1" />
              Generate
            </UButton>
          </div>

          <!-- Progress -->
          <div v-if="generating && status?.current_job" class="mt-4">
            <UProgress
              :value="status.current_job.progress || 0"
              :max="100"
              color="primary"
            />
            <p class="text-sm text-gray-500 mt-1">
              Step {{ Math.round((status.current_job.progress || 0) / 100 * steps) }}/{{ steps }}
            </p>
          </div>
        </form>
      </UCard>

      <!-- Models Management -->
      <UCard>
        <template #header>
          <h3 class="font-semibold">Models</h3>
        </template>

        <div class="divide-y">
          <div
            v-for="model in models"
            :key="model.id"
            class="flex items-center justify-between py-3"
          >
            <div>
              <div class="flex items-center gap-2">
                <p class="font-medium">{{ model.name }}</p>
                <UBadge v-if="model.downloaded" color="success" variant="soft" size="xs">
                  Downloaded
                </UBadge>
              </div>
              <p class="text-sm text-gray-500">{{ model.description }}</p>
              <p class="text-xs text-gray-400">{{ model.size }}</p>
            </div>
            <div class="flex items-center gap-2">
              <template v-if="model.downloaded">
                <UButton
                  color="error"
                  variant="soft"
                  size="xs"
                  @click="deleteModel(model.id)"
                >
                  Delete
                </UButton>
              </template>
              <template v-else>
                <UButton
                  v-if="downloadingModel === model.id"
                  :loading="true"
                  disabled
                  size="xs"
                >
                  {{ downloadProgress?.message || 'Downloading...' }}
                </UButton>
                <UButton
                  v-else
                  color="primary"
                  size="xs"
                  @click="downloadModel(model.id)"
                >
                  Download
                </UButton>
              </template>
            </div>
          </div>
        </div>
      </UCard>

      <!-- Gallery -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Gallery</h3>
            <UButton variant="ghost" size="xs" @click="refreshGenerations">
              <UIcon name="i-heroicons-arrow-path" />
            </UButton>
          </div>
        </template>

        <div v-if="!generations?.length" class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-photo" class="text-3xl mb-2" />
          <p>No images generated yet</p>
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
      </UCard>
    </div>

    <!-- Image Detail Modal -->
    <UModal v-model:open="showImageModal">
      <template v-if="selectedImage" #content>
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <h3 class="font-semibold">Image Details</h3>
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-heroicons-x-mark"
                @click="showImageModal = false"
              />
            </div>
          </template>

          <div class="space-y-4">
            <img
              v-if="selectedImage.image_url"
              :src="`/api${selectedImage.image_url}`"
              :alt="selectedImage.prompt"
              class="w-full rounded-lg"
            />

            <div class="space-y-2">
              <div>
                <p class="text-sm font-medium text-gray-500">Prompt</p>
                <p>{{ selectedImage.prompt }}</p>
              </div>

              <div class="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p class="font-medium text-gray-500">Model</p>
                  <p>{{ selectedImage.model }}</p>
                </div>
                <div>
                  <p class="font-medium text-gray-500">Size</p>
                  <p>{{ selectedImage.width }}x{{ selectedImage.height }}</p>
                </div>
                <div>
                  <p class="font-medium text-gray-500">Steps</p>
                  <p>{{ selectedImage.steps }}</p>
                </div>
                <div>
                  <p class="font-medium text-gray-500">Seed</p>
                  <p>{{ selectedImage.seed }}</p>
                </div>
              </div>

              <div>
                <p class="text-sm font-medium text-gray-500">Created</p>
                <p class="text-sm">{{ formatDate(selectedImage.created_at) }}</p>
              </div>
            </div>

            <div class="flex gap-2">
              <UButton
                color="primary"
                @click="downloadImage(selectedImage)"
              >
                <UIcon name="i-heroicons-arrow-down-tray" class="mr-1" />
                Download
              </UButton>
              <UButton
                color="error"
                variant="soft"
                @click="deleteGeneration(selectedImage.id)"
              >
                <UIcon name="i-heroicons-trash" class="mr-1" />
                Delete
              </UButton>
            </div>
          </div>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
