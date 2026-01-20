<script setup lang="ts">
definePageMeta({
  title: 'Chat'
})

const toast = useToast()

// Fetch MLX status
const { data: mlxData, refresh: refreshStatus } = await useApiFetch<{
  running: boolean
  port: number
  endpoint: string
  active_model: string
  models: { id: string; name: string }[]
}>('/mlx/models')

// Chat state
interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}
const messages = ref<ChatMessage[]>([])
const input = ref('')
const loading = ref(false)
const messagesContainer = ref<HTMLElement>()

// Auto-scroll to bottom
function scrollToBottom() {
  nextTick(() => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

// Send message
async function sendMessage() {
  if (!input.value.trim() || loading.value || !mlxData.value?.running) return

  const userMessage = input.value.trim()
  messages.value.push({ role: 'user', content: userMessage })
  input.value = ''
  loading.value = true
  scrollToBottom()

  try {
    const response = await fetch(`http://localhost:${mlxData.value.port}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        model: mlxData.value.active_model,
        messages: messages.value.map(m => ({ role: m.role, content: m.content })),
        max_tokens: 2048
      })
    })

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`)
    }

    const data = await response.json()
    const assistantMessage = data.choices?.[0]?.message?.content || 'No response'
    messages.value.push({ role: 'assistant', content: assistantMessage })
    scrollToBottom()
  } catch (error) {
    toast.add({
      title: 'Error',
      description: error instanceof Error ? error.message : 'Failed to get response',
      color: 'error'
    })
  } finally {
    loading.value = false
  }
}

// Clear chat
function clearChat() {
  messages.value = []
}

// Get model display name
const modelName = computed(() => {
  if (!mlxData.value?.active_model) return ''
  const model = mlxData.value.models?.find(m => m.id === mlxData.value?.active_model)
  return model?.name || mlxData.value.active_model.split('/').pop() || ''
})

// Refresh status periodically
let refreshTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  refreshTimer = setInterval(refreshStatus, 10000)
})
onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<template>
  <div class="h-[calc(100vh-8rem)] flex flex-col">
    <!-- Header -->
    <div class="flex items-center justify-between pb-4 border-b border-gray-200 dark:border-gray-700">
      <div class="flex items-center gap-3">
        <NuxtLink to="/llms" class="text-gray-500 hover:text-gray-700 dark:hover:text-gray-300">
          <UIcon name="i-heroicons-arrow-left" class="w-5 h-5" />
        </NuxtLink>
        <div>
          <h2 class="text-lg font-semibold">Chat</h2>
          <p v-if="mlxData?.running" class="text-sm text-gray-500">
            <span class="inline-flex items-center gap-1">
              <span class="w-2 h-2 bg-green-500 rounded-full" />
              {{ modelName }}
            </span>
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <UButton v-if="messages.length" variant="ghost" color="neutral" size="sm" @click="clearChat">
          <UIcon name="i-heroicons-trash" class="w-4 h-4 mr-1" />
          Clear
        </UButton>
      </div>
    </div>

    <!-- Not running state -->
    <div v-if="!mlxData?.running" class="flex-1 flex items-center justify-center">
      <div class="text-center">
        <UIcon name="i-heroicons-cpu-chip" class="w-12 h-12 mx-auto mb-4 text-gray-300 dark:text-gray-600" />
        <h3 class="text-lg font-medium mb-2">No Model Running</h3>
        <p class="text-gray-500 mb-4">Start a model to begin chatting</p>
        <NuxtLink to="/llms">
          <UButton>Go to LLMs</UButton>
        </NuxtLink>
      </div>
    </div>

    <!-- Chat interface -->
    <template v-else>
      <!-- Messages -->
      <div ref="messagesContainer" class="flex-1 overflow-y-auto py-4 space-y-4">
        <div v-if="!messages.length" class="h-full flex items-center justify-center text-gray-400">
          <div class="text-center">
            <UIcon name="i-heroicons-chat-bubble-left-right" class="w-12 h-12 mx-auto mb-4 opacity-50" />
            <p class="text-lg">Start a conversation</p>
            <p class="text-sm mt-1">Send a message to chat with {{ modelName }}</p>
          </div>
        </div>

        <div
          v-for="(msg, idx) in messages"
          :key="idx"
          :class="msg.role === 'user' ? 'flex justify-end' : 'flex justify-start'"
        >
          <div
            :class="[
              'max-w-[80%] px-4 py-3 rounded-2xl',
              msg.role === 'user'
                ? 'bg-primary-500 text-white rounded-br-md'
                : 'bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-bl-md'
            ]"
          >
            <p class="whitespace-pre-wrap">{{ msg.content }}</p>
          </div>
        </div>

        <!-- Loading indicator -->
        <div v-if="loading" class="flex justify-start">
          <div class="bg-gray-100 dark:bg-gray-800 px-4 py-3 rounded-2xl rounded-bl-md">
            <div class="flex items-center gap-1">
              <span class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 0ms" />
              <span class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 150ms" />
              <span class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 300ms" />
            </div>
          </div>
        </div>
      </div>

      <!-- Input -->
      <div class="pt-4 border-t border-gray-200 dark:border-gray-700">
        <form @submit.prevent="sendMessage" class="flex gap-3">
          <UTextarea
            v-model="input"
            placeholder="Type a message..."
            class="flex-1"
            :rows="1"
            autoresize
            :maxrows="5"
            :disabled="loading"
            @keydown.enter.exact.prevent="sendMessage"
          />
          <UButton type="submit" size="lg" :loading="loading" :disabled="!input.trim()">
            <UIcon name="i-heroicons-paper-airplane" class="w-5 h-5" />
          </UButton>
        </form>
        <p class="text-xs text-gray-400 mt-2 text-center">
          {{ modelName }} may produce inaccurate information
        </p>
      </div>
    </template>
  </div>
</template>
