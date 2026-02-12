<script setup lang="ts">
const open = defineModel<boolean>({ default: false })

const toast = useToast()
const route = useRoute()

interface MLXModel {
  id: string
  name: string
  size?: string
  category?: string
  downloaded?: boolean
}

interface MLXData {
  running: boolean
  port: number
  endpoint: string
  active_model: string
  models: MLXModel[]
}

interface AssistantMessage {
  role: 'user' | 'assistant'
  content: string
}

const ASSISTANT_MODEL_ID = 'mlx-community/functiongemma-270m-it-bf16'

const messages = ref<AssistantMessage[]>([])
const input = ref('')
const loading = ref(false)
const messagesContainer = ref<HTMLElement>()

// Model setup state
const settingUpModel = ref(false)
const modelSetupProgress = ref(0)
const modelSetupStatus = ref('')

// Fetch MLX status once
const { data: mlxData, refresh: refreshStatus } = await useApiFetch<MLXData>('/mlx/models')

const isAssistantModelReady = computed(() =>
  mlxData.value?.models?.some(m => m.id === ASSISTANT_MODEL_ID && m.downloaded) ?? false
)

// Page context from current route
const pageContext = computed(() => {
  const path = route.path
  if (path === '/') return 'Dashboard'
  if (path.startsWith('/apps')) return 'Apps'
  if (path.startsWith('/templates')) return 'One-Click Apps'
  if (path.startsWith('/llms')) return 'One-Click LLMs'
  if (path.startsWith('/chat')) return 'Chat'
  if (path.startsWith('/processes')) return 'Processes'
  if (path.startsWith('/activity')) return 'Activity'
  if (path.startsWith('/storage')) return 'Storage'
  if (path.startsWith('/settings')) return 'Settings'
  if (path.startsWith('/docs')) return 'Documentation'
  return ''
})

function scrollToBottom() {
  nextTick(() => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

async function send() {
  if (!input.value.trim() || loading.value) return

  const userMessage = input.value.trim()
  messages.value.push({ role: 'user', content: userMessage })
  input.value = ''
  loading.value = true
  scrollToBottom()

  try {
    const result = await $api<{
      response: string
      action?: { function: string; parameters: Record<string, unknown>; success: boolean }
    }>('/ai/ask', {
      method: 'POST',
      body: { message: userMessage, context: pageContext.value }
    })

    let content = result.response
    if (result.action) {
      const badge = result.action.success ? `[${result.action.function}]` : `[${result.action.function} FAILED]`
      content = `${badge} ${content}`
    }

    messages.value.push({ role: 'assistant', content })
    scrollToBottom()
  } catch (error: any) {
    const errorMsg = error?.data?.error || error?.message || 'Failed to get response'
    messages.value.pop()
    if (errorMsg.includes('not downloaded') || errorMsg.includes('not available')) {
      await refreshStatus()
      toast.add({ title: 'Assistant not ready', description: 'Click "Setup Assistant" to get started', color: 'warning' })
    } else {
      toast.add({ title: 'Error', description: errorMsg, color: 'error' })
    }
  } finally {
    loading.value = false
  }
}

function clearChat() {
  messages.value = []
}

async function setupModel() {
  if (settingUpModel.value) return

  const modelName = ASSISTANT_MODEL_ID.split('/').pop() || ASSISTANT_MODEL_ID
  settingUpModel.value = true
  modelSetupStatus.value = 'Starting download...'
  modelSetupProgress.value = 0

  try {
    const alreadyDownloaded = mlxData.value?.models?.some(m => m.id === ASSISTANT_MODEL_ID && m.downloaded)

    if (!alreadyDownloaded) {
      await $api('/mlx/pull', {
        method: 'POST',
        body: { model: ASSISTANT_MODEL_ID }
      })

      modelSetupStatus.value = `Downloading ${modelName}...`

      await new Promise<void>((resolve, reject) => {
        const pollInterval = setInterval(async () => {
          try {
            const progress = await $api<{
              status: string
              progress: number
              bytes_total: number
              bytes_done: number
              speed: number
              message: string
            }>(`/mlx/pull/progress?model=${encodeURIComponent(ASSISTANT_MODEL_ID)}`)

            if (progress) {
              const pct = progress.bytes_total > 0
                ? (progress.bytes_done / progress.bytes_total) * 100
                : (progress.progress || 0)
              modelSetupProgress.value = pct

              const doneMB = (progress.bytes_done / 1024 / 1024).toFixed(1)
              const totalMB = (progress.bytes_total / 1024 / 1024).toFixed(0)
              if (progress.speed > 0) {
                const speedMB = (progress.speed / 1024 / 1024).toFixed(1)
                modelSetupStatus.value = `Downloading... ${doneMB}/${totalMB} MB (${speedMB} MB/s)`
              } else {
                modelSetupStatus.value = `Downloading... ${doneMB}/${totalMB} MB`
              }

              if (progress.status === 'completed') {
                clearInterval(pollInterval)
                resolve()
              } else if (progress.status === 'error' || progress.status === 'cancelled') {
                clearInterval(pollInterval)
                reject(new Error(progress.message || 'Download failed'))
              }
            }
          } catch {
            // Progress endpoint might not have data yet
          }
        }, 1000)

        setTimeout(() => {
          clearInterval(pollInterval)
          reject(new Error('Download timed out'))
        }, 60 * 60 * 1000)
      })
    }

    modelSetupStatus.value = 'Starting assistant...'
    modelSetupProgress.value = 100

    try {
      await $api('/ai/ask', { method: 'POST', body: { message: 'hello' } })
    } catch {
      // First call may be slow
    }

    await refreshStatus()
    toast.add({ title: 'Assistant ready', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Setup failed', description: error?.message || 'Unknown error', color: 'error' })
  } finally {
    settingUpModel.value = false
    modelSetupProgress.value = 0
    modelSetupStatus.value = ''
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

// Simple markdown renderer for assistant messages
function renderMarkdown(text: string): string {
  // Escape HTML first
  let html = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')

  // Code blocks: ```...```
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, '<pre class="bg-gray-950 text-gray-300 p-2 rounded text-xs my-1 overflow-x-auto"><code>$2</code></pre>')

  // Inline code: `...`
  html = html.replace(/`([^`]+)`/g, '<code class="bg-gray-200 dark:bg-gray-700 px-1 rounded text-xs">$1</code>')

  // Bold: **...**
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')

  // Process lines for lists and paragraphs
  const lines = html.split('\n')
  const result: string[] = []
  let inList = false

  for (const line of lines) {
    // Skip lines inside pre blocks (already handled)
    if (line.includes('<pre') || line.includes('</pre>')) {
      if (inList) { result.push('</ul>'); inList = false }
      result.push(line)
      continue
    }
    // Bullet list items: - ...
    if (/^- /.test(line.trim())) {
      if (!inList) { result.push('<ul class="list-disc list-inside space-y-0.5">'); inList = true }
      result.push(`<li>${line.trim().slice(2)}</li>`)
    } else {
      if (inList) { result.push('</ul>'); inList = false }
      if (line.trim() === '') {
        result.push('<br/>')
      } else {
        result.push(`<span>${line}</span><br/>`)
      }
    }
  }
  if (inList) result.push('</ul>')

  // Clean up trailing <br/>
  let final = result.join('\n')
  final = final.replace(/(<br\/>\s*)+$/, '')
  return final
}
</script>

<template>
  <USlideover v-model:open="open" title="Assistant" :overlay="false">
    <template #body>
      <div class="flex flex-col h-full">
        <!-- Page context badge -->
        <div v-if="pageContext" class="mb-3">
          <UBadge color="primary" variant="subtle" size="xs">
            {{ pageContext }}
          </UBadge>
        </div>

        <!-- Messages area -->
        <div ref="messagesContainer" class="flex-1 overflow-y-auto space-y-3">
          <!-- Empty state -->
          <div v-if="!messages.length" class="h-full flex items-center justify-center text-dimmed">
            <div class="text-center">
              <UIcon name="i-heroicons-sparkles" class="w-10 h-10 mx-auto mb-3 opacity-50" />

              <!-- Setup needed -->
              <template v-if="!isAssistantModelReady && !settingUpModel">
                <p class="text-base font-medium">Setup Assistant</p>
                <p class="text-sm mt-1 text-dimmed">FunctionGemma is required (150MB)</p>
                <UButton class="mt-4" @click="setupModel()">
                  <UIcon name="i-heroicons-arrow-down-tray" class="w-4 h-4 mr-2" />
                  Setup Assistant
                </UButton>
              </template>

              <!-- Setting up -->
              <template v-else-if="settingUpModel">
                <p class="text-sm mt-2 text-dimmed">{{ modelSetupStatus }}</p>
                <div class="mt-3 w-48 mx-auto">
                  <div class="h-2 bg-(--ui-bg-muted) rounded-full overflow-hidden">
                    <div
                      class="h-full bg-primary-500 rounded-full transition-all duration-500"
                      :style="{ width: `${Math.max(modelSetupProgress, 2)}%` }"
                    />
                  </div>
                  <p class="text-xs text-dimmed mt-1">{{ modelSetupProgress.toFixed(1) }}%</p>
                </div>
              </template>

              <!-- Ready -->
              <template v-else>
                <p class="text-base font-medium">How can I help?</p>
                <p class="text-sm mt-1">Manage apps, check status, view logs</p>
                <div class="mt-3 flex flex-wrap gap-2 justify-center">
                  <button
                    v-for="hint in ['list my apps', 'storage info', 'system info']"
                    :key="hint"
                    class="px-3 py-1 text-xs bg-(--ui-bg-muted) rounded-full hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
                    @click="input = hint; send()"
                  >
                    {{ hint }}
                  </button>
                </div>
              </template>
            </div>
          </div>

          <!-- Messages -->
          <div
            v-for="(msg, idx) in messages"
            :key="idx"
            :class="msg.role === 'user' ? 'flex justify-end' : 'flex justify-start'"
          >
            <div
              :class="[
                'max-w-[85%] rounded-2xl px-4 py-2.5',
                msg.role === 'user'
                  ? 'bg-primary-500 text-white rounded-br-md'
                  : 'bg-(--ui-bg-muted) rounded-bl-md'
              ]"
            >
              <p v-if="msg.role === 'user'" class="text-sm whitespace-pre-wrap">{{ msg.content }}</p>
              <div v-else class="text-sm assistant-content" v-html="renderMarkdown(msg.content)" />
            </div>
          </div>

          <!-- Loading indicator -->
          <div v-if="loading" class="flex justify-start">
            <div class="bg-(--ui-bg-muted) rounded-2xl rounded-bl-md px-4 py-2.5">
              <div class="flex gap-1">
                <span class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 0ms" />
                <span class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 150ms" />
                <span class="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style="animation-delay: 300ms" />
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex flex-col gap-2">
        <!-- Clear chat button -->
        <div v-if="messages.length" class="flex justify-end">
          <UButton
            variant="ghost"
            color="neutral"
            size="xs"
            icon="i-heroicons-trash"
            @click="clearChat"
          >
            Clear
          </UButton>
        </div>
        <!-- Input -->
        <div class="flex gap-2">
          <UTextarea
            v-model="input"
            :rows="1"
            autoresize
            placeholder="Ask me anything..."
            class="flex-1"
            @keydown="handleKeydown"
          />
          <UButton
            icon="i-heroicons-paper-airplane"
            color="primary"
            :disabled="!input.trim() || loading"
            @click="send"
          />
        </div>
      </div>
    </template>
  </USlideover>
</template>
