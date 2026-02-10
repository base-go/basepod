<script setup lang="ts">
import type { MLXModel, MLXModelsResponse, MLXDownloadProgress } from '~/types'

definePageMeta({
  title: 'LLMs'
})

const toast = useToast()

// Fetch models and status
const { data: mlxData, refresh: refreshModels } = await useApiFetch<MLXModelsResponse>('/mlx/models')

// Pulling state with progress
const pullingModels = ref<Set<string>>(new Set())
const downloadProgress = ref<Map<string, MLXDownloadProgress>>(new Map())

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

// Get model catalog with descriptions
const modelCatalog: Record<string, string> = {
  'mlx-community/Llama-3.2-1B-Instruct-4bit': 'Ultra-fast, great for quick tasks',
  'mlx-community/Llama-3.2-3B-Instruct-4bit': 'Fast and capable',
  'mlx-community/Qwen2.5-3B-Instruct-4bit': 'Strong multilingual support',
  'mlx-community/Qwen2.5-7B-Instruct-4bit': 'Powerful general purpose',
  'mlx-community/Qwen2.5-Coder-7B-Instruct-4bit': 'Optimized for code',
  'mlx-community/Mistral-7B-Instruct-v0.3-4bit': 'Strong reasoning',
  'mlx-community/gemma-2-9b-it-4bit': "Google's latest",
  'mlx-community/Phi-3.5-mini-instruct-4bit': "Microsoft's efficient model",
  'mlx-community/DeepSeek-Coder-V2-Lite-Instruct-4bit': 'Coding specialist'
}

function getDescription(modelId: string): string {
  return modelCatalog[modelId] || ''
}

// Category display info
const categoryInfo: Record<string, { label: string; icon: string; description: string }> = {
  chat: { label: 'Chat', icon: 'i-heroicons-chat-bubble-left-right', description: 'General purpose conversation' },
  code: { label: 'Code', icon: 'i-heroicons-code-bracket', description: 'Programming and code generation' },
  reasoning: { label: 'Reasoning', icon: 'i-heroicons-light-bulb', description: 'Chain-of-thought reasoning' },
  vision: { label: 'Vision', icon: 'i-heroicons-eye', description: 'Image understanding' },
  embedding: { label: 'Embedding', icon: 'i-heroicons-cube-transparent', description: 'Text to vector embeddings' },
  speech: { label: 'Speech', icon: 'i-heroicons-microphone', description: 'Audio transcription' }
}

// Get ordered categories
const categories = ['chat', 'code', 'reasoning', 'vision', 'embedding', 'speech']

// Models grouped by category
const modelsByCategory = computed(() => {
  if (!mlxData.value?.models) return {}

  const grouped: Record<string, MLXModel[]> = {}

  for (const category of categories) {
    const categoryModels = mlxData.value.models
      .filter(m => m.category === category)
      .sort((a, b) => {
        // Running model first
        if (mlxData.value?.active_model === a.id) return -1
        if (mlxData.value?.active_model === b.id) return 1
        // Downloaded before not downloaded
        if (a.downloaded && !b.downloaded) return -1
        if (!a.downloaded && b.downloaded) return 1
        // Then by name
        return a.name.localeCompare(b.name)
      })
    if (categoryModels.length > 0) {
      grouped[category] = categoryModels
    }
  }

  return grouped
})

// Categories with models (for rendering)
const activeCategories = computed(() => {
  return categories.filter(c => (modelsByCategory.value[c]?.length ?? 0) > 0)
})

// Model detail modal
const selectedModel = ref<MLXModel | null>(null)
const showModelModal = ref(false)

function openModelDetail(model: MLXModel) {
  selectedModel.value = model
  showModelModal.value = true
}

function closeModelModal() {
  showModelModal.value = false
  selectedModel.value = null
}

// Pull a model
async function pullModel(modelId: string) {
  pullingModels.value.add(modelId)
  try {
    await $api('/mlx/pull', {
      method: 'POST',
      body: { model: modelId }
    })
    toast.add({
      title: 'Downloading model',
      description: 'Download started...',
      color: 'info'
    })
    // Poll for progress
    const pollInterval = setInterval(async () => {
      try {
        const progress = await $api<MLXDownloadProgress>(`/mlx/pull/progress?model=${encodeURIComponent(modelId)}`)
        if (progress) {
          downloadProgress.value.set(modelId, progress)

          if (progress.status === 'completed') {
            clearInterval(pollInterval)
            pullingModels.value.delete(modelId)
            downloadProgress.value.delete(modelId)
            await refreshModels()
            const model = mlxData.value?.models.find(m => m.id === modelId)
            toast.add({
              title: 'Model ready',
              description: `${model?.name || modelId} is ready to use`,
              color: 'success'
            })
          } else if (progress.status === 'error' || progress.status === 'cancelled') {
            clearInterval(pollInterval)
            pullingModels.value.delete(modelId)
            downloadProgress.value.delete(modelId)
            toast.add({
              title: 'Download failed',
              description: progress.message || 'Unknown error',
              color: 'error'
            })
          }
        }
      } catch {
        // Progress endpoint might not have data yet, continue polling
      }
    }, 1000)
    // Timeout after 60 minutes
    setTimeout(() => {
      clearInterval(pollInterval)
      pullingModels.value.delete(modelId)
      downloadProgress.value.delete(modelId)
    }, 60 * 60 * 1000)
  } catch (error) {
    pullingModels.value.delete(modelId)
    downloadProgress.value.delete(modelId)
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'Failed to pull model'
    toast.add({ title: 'Error', description: message, color: 'error' })
  }
}

// Run a model
async function runModel(modelId: string) {
  try {
    await $api('/mlx/run', {
      method: 'POST',
      body: { model: modelId }
    })
    await refreshModels()
    toast.add({
      title: 'Model running',
      description: 'LLM server is ready',
      color: 'success'
    })
  } catch (error) {
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'Failed to run model'
    toast.add({ title: 'Error', description: message, color: 'error' })
  }
}

// Stop the server
async function stopServer() {
  try {
    await $api('/mlx/stop', { method: 'POST' })
    await refreshModels()
    toast.add({ title: 'Server stopped', color: 'success' })
  } catch (error) {
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'Failed to stop server'
    toast.add({ title: 'Error', description: message, color: 'error' })
  }
}

// Custom model
const customModelId = ref('')
const customModelLoading = ref(false)

async function pullCustomModel() {
  if (!customModelId.value.trim()) return

  const modelId = customModelId.value.trim()
  customModelLoading.value = true
  pullingModels.value.add(modelId)

  try {
    await $api('/mlx/pull', {
      method: 'POST',
      body: { model: modelId }
    })
    toast.add({
      title: 'Downloading custom model',
      description: `Pulling ${modelId}...`,
      color: 'info'
    })

    // Poll for progress
    const pollInterval = setInterval(async () => {
      try {
        const progress = await $api<MLXDownloadProgress>(`/mlx/pull/progress?model=${encodeURIComponent(modelId)}`)
        if (progress) {
          downloadProgress.value.set(modelId, progress)

          if (progress.status === 'completed') {
            clearInterval(pollInterval)
            pullingModels.value.delete(modelId)
            downloadProgress.value.delete(modelId)
            customModelLoading.value = false
            await refreshModels()
            toast.add({
              title: 'Model ready',
              description: `${modelId} is ready to use`,
              color: 'success'
            })
            customModelId.value = ''
          } else if (progress.status === 'error' || progress.status === 'cancelled') {
            clearInterval(pollInterval)
            pullingModels.value.delete(modelId)
            downloadProgress.value.delete(modelId)
            customModelLoading.value = false
            toast.add({
              title: 'Download failed',
              description: progress.message || 'Unknown error',
              color: 'error'
            })
          }
        }
      } catch {
        // Progress endpoint might not have data yet
      }
    }, 1000)

    // Timeout after 60 minutes
    setTimeout(() => {
      clearInterval(pollInterval)
      pullingModels.value.delete(modelId)
      downloadProgress.value.delete(modelId)
      customModelLoading.value = false
    }, 60 * 60 * 1000)
  } catch (error) {
    pullingModels.value.delete(modelId)
    customModelLoading.value = false
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'Failed to pull model'
    toast.add({ title: 'Error', description: message, color: 'error' })
  }
}

// Auto-refresh status
let refreshTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  refreshTimer = setInterval(() => {
    refreshModels()
  }, 10000)
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
        <h2 class="text-xl font-semibold">Local LLMs</h2>
        <p class="text-gray-500 dark:text-gray-400">
          Run AI models locally on Apple Silicon
        </p>
      </div>
      <div v-if="mlxData?.running" class="flex items-center gap-3">
        <div class="flex items-center gap-2 px-3 py-2 bg-green-100 dark:bg-green-900/30 rounded-lg">
          <span class="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
          <span class="text-sm font-medium text-green-700 dark:text-green-300">
            Running on :{{ mlxData.port }}
          </span>
        </div>
        <UButton variant="soft" color="error" size="sm" @click="stopServer">
          Stop Server
        </UButton>
      </div>
    </div>

    <!-- Not Supported Warning -->
    <div v-if="!mlxData?.supported" class="p-6 bg-amber-50 dark:bg-amber-900/20 rounded-lg mb-6 border border-amber-200 dark:border-amber-800">
      <div class="flex items-start gap-4">
        <UIcon name="i-heroicons-exclamation-triangle" class="w-8 h-8 text-amber-500 flex-shrink-0" />
        <div>
          <h3 class="font-semibold text-amber-800 dark:text-amber-200 text-lg">Local LLMs Not Available</h3>
          <p class="text-amber-700 dark:text-amber-300 mt-1">
            {{ mlxData?.unsupported_reason || 'MLX requires macOS with Apple Silicon (M series).' }}
          </p>
          <p v-if="mlxData?.platform" class="text-sm text-amber-600 dark:text-amber-400 mt-2">
            Current platform: <code class="bg-amber-100 dark:bg-amber-800 px-1.5 py-0.5 rounded">{{ mlxData.platform }}</code>
          </p>
          <div class="mt-4 p-3 bg-amber-100 dark:bg-amber-800/50 rounded text-sm">
            <p class="text-amber-800 dark:text-amber-200">
              <strong>To use local LLMs:</strong> Run Basepod on a Mac with Apple Silicon.
              MLX models leverage the unified memory architecture of M-series chips for efficient inference.
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Custom Model Input -->
    <div v-if="mlxData?.supported" class="mb-6 p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
      <div class="flex items-center gap-2 mb-3">
        <UIcon name="i-heroicons-plus-circle" class="w-5 h-5 text-gray-500" />
        <h3 class="font-medium">Run Custom Model</h3>
      </div>
      <p class="text-sm text-gray-500 mb-3">
        Paste any MLX model ID from <a href="https://huggingface.co/mlx-community" target="_blank" class="text-primary-500 hover:underline">Hugging Face</a>
      </p>
      <div class="flex gap-2">
        <UInput
          v-model="customModelId"
          placeholder="mlx-community/MiMo-V2-Flash-mlx-8bit"
          class="flex-1 font-mono text-sm"
          :disabled="customModelLoading"
        />
        <UButton
          :loading="customModelLoading"
          :disabled="!customModelId.trim()"
          @click="pullCustomModel"
        >
          Pull & Run
        </UButton>
      </div>
      <!-- Progress for custom model -->
      <div v-if="customModelLoading && downloadProgress.get(customModelId)" class="mt-3">
        <div class="flex items-center justify-between text-xs text-gray-500 mb-1">
          <span>{{ Math.round(downloadProgress.get(customModelId)!.progress) }}%</span>
          <span v-if="downloadProgress.get(customModelId)?.eta">
            ETA: {{ formatETA(downloadProgress.get(customModelId)!.eta) }}
          </span>
        </div>
        <div class="h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
          <div
            class="h-full bg-primary-500 transition-all duration-300"
            :style="{ width: `${downloadProgress.get(customModelId)?.progress || 0}%` }"
          />
        </div>
      </div>
    </div>

    <!-- Active Model Banner -->
    <div v-if="mlxData?.active_model" class="mb-6 p-4 bg-orange-50 dark:bg-orange-900/20 rounded-lg border border-orange-200 dark:border-orange-800">
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-3">
          <UIcon name="i-heroicons-cpu-chip" class="w-6 h-6 text-orange-500" />
          <div>
            <div class="text-sm text-orange-600 dark:text-orange-400">Active Model</div>
            <div class="font-medium">{{ mlxData?.models?.find(m => m.id === mlxData?.active_model)?.name || mlxData?.active_model }}</div>
          </div>
        </div>
        <div class="flex items-center gap-3">
          <code class="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded text-gray-500">{{ mlxData?.endpoint }}</code>
          <NuxtLink to="/chat">
            <UButton size="sm">
              <UIcon name="i-heroicons-chat-bubble-left-right" class="w-4 h-4 mr-1" />
              Open Chat
            </UButton>
          </NuxtLink>
        </div>
      </div>
    </div>

    <!-- Models by Category -->
    <div class="space-y-8">
      <div v-for="category in activeCategories" :key="category">
        <!-- Category Header -->
        <div class="mb-4">
          <div class="flex items-center gap-2 mb-1">
            <UIcon :name="categoryInfo[category]?.icon" class="w-5 h-5 text-gray-600 dark:text-gray-400" />
            <h3 class="text-lg font-semibold">{{ categoryInfo[category]?.label }}</h3>
          </div>
          <p class="text-sm text-gray-500">{{ categoryInfo[category]?.description }}</p>
        </div>

        <!-- Models Grid for this category -->
        <div class="space-y-3">
          <div
            v-for="model in modelsByCategory[category]"
            :key="model.id"
            class="p-4 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
            :class="{
              'ring-2 ring-orange-500': mlxData?.active_model === model.id,
              'opacity-60': !mlxData?.supported
            }"
          >
            <div class="flex items-center justify-between">
              <div
                class="flex items-center gap-4 cursor-pointer hover:opacity-80 transition-opacity flex-1"
                @click="openModelDetail(model)"
              >
                <div class="w-10 h-10 bg-orange-100 dark:bg-orange-900/50 rounded-lg flex items-center justify-center">
                  <UIcon :name="categoryInfo[category]?.icon" class="w-5 h-5 text-orange-500" />
                </div>
                <div>
                  <div class="font-medium flex items-center gap-2">
                    {{ model.name }}
                    <span v-if="mlxData?.active_model === model.id" class="text-xs bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 px-2 py-0.5 rounded">
                      Running
                    </span>
                    <span v-else-if="model.downloaded" class="text-xs bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 px-2 py-0.5 rounded">
                      Downloaded
                    </span>
                  </div>
                  <div class="text-sm text-gray-500">{{ model.description || getDescription(model.id) }}</div>
                </div>
              </div>
              <div class="flex items-center gap-3">
                <span class="text-sm text-gray-400">{{ model.size }}</span>

                <!-- Actions -->
                <template v-if="mlxData?.supported">
                  <!-- If pulling - show progress -->
                  <div v-if="pullingModels.has(model.id)" class="flex items-center gap-3 min-w-[200px]">
                    <div class="flex-1">
                      <div class="flex items-center justify-between text-xs text-gray-500 mb-1">
                        <span v-if="downloadProgress.get(model.id)?.progress">
                          {{ Math.round(downloadProgress.get(model.id)!.progress) }}%
                        </span>
                        <span v-else>Starting...</span>
                        <span v-if="downloadProgress.get(model.id)?.eta">
                          ETA: {{ formatETA(downloadProgress.get(model.id)!.eta) }}
                        </span>
                      </div>
                      <div class="h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                        <div
                          class="h-full bg-orange-500 transition-all duration-300"
                          :style="{ width: `${downloadProgress.get(model.id)?.progress || 0}%` }"
                        />
                      </div>
                      <div class="flex items-center justify-between text-xs text-gray-400 mt-1">
                        <span v-if="downloadProgress.get(model.id)?.bytes_done">
                          {{ formatBytes(downloadProgress.get(model.id)!.bytes_done) }} / {{ formatBytes(downloadProgress.get(model.id)!.bytes_total) }}
                        </span>
                        <span v-if="downloadProgress.get(model.id)?.speed">
                          {{ formatBytes(downloadProgress.get(model.id)!.speed) }}/s
                        </span>
                      </div>
                    </div>
                  </div>

                  <!-- If running this model -->
                  <UButton
                    v-else-if="mlxData?.active_model === model.id"
                    size="sm"
                    variant="soft"
                    color="error"
                    @click="stopServer"
                  >
                    Stop
                  </UButton>

                  <!-- If downloaded but not running -->
                  <UButton
                    v-else-if="model.downloaded"
                    size="sm"
                    @click="runModel(model.id)"
                  >
                    Run
                  </UButton>

                  <!-- Not downloaded -->
                  <UButton
                    v-else
                    size="sm"
                    variant="soft"
                    @click="pullModel(model.id)"
                  >
                    Pull
                  </UButton>
                </template>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Usage Info -->
    <div v-if="mlxData?.running" class="mt-6 p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
      <h3 class="font-medium mb-2">API Usage</h3>
      <p class="text-sm text-gray-600 dark:text-gray-400 mb-2">
        The MLX server is OpenAI-compatible. Use it with any OpenAI client:
      </p>
      <pre class="text-xs bg-gray-900 text-gray-100 p-3 rounded overflow-x-auto">curl {{ mlxData.endpoint }} \
  -H "Content-Type: application/json" \
  -d '{"model": "{{ mlxData.active_model }}", "messages": [{"role": "user", "content": "Hello!"}]}'</pre>
    </div>

    <!-- Model Detail Modal -->
    <UModal v-model:open="showModelModal">
      <template #content>
        <div v-if="selectedModel" class="p-6">
          <div class="flex items-start justify-between mb-4">
            <div class="flex items-center gap-3">
              <div class="w-12 h-12 bg-orange-100 dark:bg-orange-900/50 rounded-lg flex items-center justify-center">
                <UIcon :name="categoryInfo[selectedModel.category]?.icon || 'i-heroicons-cpu-chip'" class="w-6 h-6 text-orange-500" />
              </div>
              <div>
                <h3 class="text-lg font-semibold">{{ selectedModel.name }}</h3>
                <p class="text-sm text-gray-500">{{ selectedModel.description }}</p>
              </div>
            </div>
            <UButton variant="ghost" size="sm" icon="i-heroicons-x-mark" @click="closeModelModal" />
          </div>

          <!-- Model Info -->
          <div class="space-y-4">
            <div class="grid grid-cols-2 gap-4">
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Size</div>
                <div class="font-medium">{{ selectedModel.size }}</div>
              </div>
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Category</div>
                <div class="font-medium capitalize">{{ selectedModel.category }}</div>
              </div>
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Status</div>
                <div class="font-medium">
                  <span v-if="mlxData?.active_model === selectedModel.id" class="text-green-600">Running</span>
                  <span v-else-if="selectedModel.downloaded" class="text-blue-600">Downloaded</span>
                  <span v-else class="text-gray-500">Not Downloaded</span>
                </div>
              </div>
              <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                <div class="text-xs text-gray-500 mb-1">Required RAM</div>
                <div class="font-medium">{{ selectedModel.required_ram_gb || '~' }}GB</div>
              </div>
            </div>

            <!-- Model ID -->
            <div class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <div class="text-xs text-gray-500 mb-1">Model ID</div>
              <code class="text-xs break-all">{{ selectedModel.id }}</code>
            </div>

            <!-- Actions -->
            <div class="flex gap-3 pt-2">
              <template v-if="mlxData?.supported">
                <UButton
                  v-if="mlxData?.active_model === selectedModel.id"
                  variant="soft"
                  color="error"
                  class="flex-1"
                  @click="stopServer(); closeModelModal()"
                >
                  Stop Server
                </UButton>
                <UButton
                  v-else-if="selectedModel.downloaded"
                  class="flex-1"
                  @click="runModel(selectedModel.id); closeModelModal()"
                >
                  Run Model
                </UButton>
                <UButton
                  v-else-if="!pullingModels.has(selectedModel.id)"
                  variant="soft"
                  class="flex-1"
                  @click="pullModel(selectedModel.id); closeModelModal()"
                >
                  Download Model
                </UButton>
                <UButton
                  v-else
                  variant="soft"
                  class="flex-1"
                  disabled
                >
                  Downloading...
                </UButton>
              </template>
              <UButton variant="outline" @click="closeModelModal">
                Close
              </UButton>
            </div>
          </div>
        </div>
      </template>
    </UModal>
  </div>
</template>
