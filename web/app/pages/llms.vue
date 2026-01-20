<script setup lang="ts">
import type { MLXModelsResponse } from '~/types'

definePageMeta({
  title: 'LLMs'
})

const toast = useToast()

// Fetch models and status
const { data: mlxData, refresh: refreshModels } = await useApiFetch<MLXModelsResponse>('/mlx/models')

// Pulling state
const pullingModels = ref<Set<string>>(new Set())

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
      description: 'This may take several minutes...',
      color: 'info'
    })
    // Poll for completion
    const pollInterval = setInterval(async () => {
      await refreshModels()
      const model = mlxData.value?.models.find(m => m.id === modelId)
      if (model?.downloaded) {
        clearInterval(pollInterval)
        pullingModels.value.delete(modelId)
        toast.add({
          title: 'Model ready',
          description: `${model.name} is ready to use`,
          color: 'success'
        })
      }
    }, 5000)
    // Timeout after 30 minutes
    setTimeout(() => {
      clearInterval(pollInterval)
      pullingModels.value.delete(modelId)
    }, 30 * 60 * 1000)
  } catch (error) {
    pullingModels.value.delete(modelId)
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
      description: `Server available at localhost:${mlxData.value?.port}`,
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
    <div v-if="!mlxData?.supported" class="p-6 bg-yellow-50 dark:bg-yellow-900/20 rounded-lg mb-6">
      <div class="flex items-center gap-3">
        <UIcon name="i-heroicons-exclamation-triangle" class="w-6 h-6 text-yellow-500" />
        <div>
          <h3 class="font-medium text-yellow-800 dark:text-yellow-200">MLX Not Available</h3>
          <p class="text-sm text-yellow-700 dark:text-yellow-300">
            Local LLMs require macOS with Apple Silicon (M1/M2/M3/M4).
          </p>
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
            <div class="font-medium">{{ mlxData.models.find(m => m.id === mlxData.active_model)?.name || mlxData.active_model }}</div>
          </div>
        </div>
        <div class="flex items-center gap-3">
          <code class="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded text-gray-500">{{ mlxData.endpoint }}</code>
          <NuxtLink to="/chat">
            <UButton size="sm">
              <UIcon name="i-heroicons-chat-bubble-left-right" class="w-4 h-4 mr-1" />
              Open Chat
            </UButton>
          </NuxtLink>
        </div>
      </div>
    </div>

    <!-- Models Grid -->
    <div class="space-y-3">
      <div
        v-for="model in mlxData?.models"
        :key="model.id"
        class="p-4 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700"
        :class="{
          'ring-2 ring-orange-500': mlxData?.active_model === model.id,
          'opacity-60': !mlxData?.supported
        }"
      >
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-4">
            <div class="w-10 h-10 bg-orange-100 dark:bg-orange-900/50 rounded-lg flex items-center justify-center">
              <UIcon name="i-heroicons-cpu-chip" class="w-5 h-5 text-orange-500" />
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
              <div class="text-sm text-gray-500">{{ getDescription(model.id) }}</div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <span class="text-sm text-gray-400">{{ model.size }}</span>

            <!-- Actions -->
            <template v-if="mlxData?.supported">
              <!-- If pulling -->
              <UButton v-if="pullingModels.has(model.id)" disabled size="sm" variant="soft">
                <UIcon name="i-heroicons-arrow-path" class="w-4 h-4 animate-spin mr-1" />
                Pulling...
              </UButton>

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
  </div>
</template>
