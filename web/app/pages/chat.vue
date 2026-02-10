<script setup lang="ts">
definePageMeta({
  title: 'Chat'
})

const toast = useToast()

// Model types
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

// Multimodal content types
interface TextContent {
  type: 'text'
  text: string
}

interface ImageContent {
  type: 'image_url'
  image_url: { url: string }
}

type MessageContent = TextContent | ImageContent

interface ChatMessage {
  role: 'user' | 'assistant'
  content: string | MessageContent[]
}

// Fetch MLX status
const { data: mlxData, refresh: refreshStatus } = await useApiFetch<MLXData>('/mlx/models')

// Chat state
const messages = ref<ChatMessage[]>([])
const input = ref('')
const loading = ref(false)
const loadingStatus = ref('') // Status message during loading
const loadingDuration = ref(0) // Seconds elapsed
const messagesContainer = ref<HTMLElement>()
let loadingTimer: ReturnType<typeof setInterval> | null = null

// Load chat history from database when model changes
watch(() => mlxData.value?.active_model, async (newModel) => {
  if (!newModel) return
  await loadChatHistory(newModel)
}, { immediate: true })

async function loadChatHistory(modelId: string) {
  try {
    const data = await $api<{ id: number; role: string; content: string }[]>(
      `/chat/messages/${encodeURIComponent(modelId)}`
    )
    if (data && Array.isArray(data)) {
      messages.value = data.map(m => ({
        role: m.role as 'user' | 'assistant',
        content: m.content
      }))
      scrollToBottom()
    }
  } catch (error) {
    // Ignore load errors, start fresh
    messages.value = []
  }
}

// Save message to database
async function saveMessageToDb(role: string, content: string) {
  if (!mlxData.value?.active_model) return
  try {
    await $api(`/chat/messages/${encodeURIComponent(mlxData.value.active_model)}`, {
      method: 'POST',
      body: { role, content }
    })
  } catch {
    // Ignore save errors
  }
}

// Model selector state
const showModelSelector = ref(false)
const switchingModel = ref(false)

// Image upload state
const uploadedImages = ref<{ url: string; preview: string; fullUrl?: string }[]>([])
const isDragging = ref(false)
const fileInputRef = ref<HTMLInputElement>()
const maxImages = 4
const analysisMode = ref<'quick' | 'deep'>('quick')
const lastImageMessageIndex = ref<number | null>(null) // Track last image message for "analyze deeper"

// Voice input state
const isRecording = ref(false)
const transcribing = ref(false)
const mediaRecorder = ref<MediaRecorder | null>(null)
const audioChunks = ref<Blob[]>([])

// Computed properties
const downloadedModels = computed(() =>
  mlxData.value?.models?.filter(m => m.downloaded) || []
)

const modelName = computed(() => {
  if (!mlxData.value?.active_model) return ''
  const model = mlxData.value.models?.find(m => m.id === mlxData.value?.active_model)
  return model?.name || mlxData.value.active_model.split('/').pop() || ''
})

const currentModel = computed(() =>
  mlxData.value?.models?.find(m => m.id === mlxData.value?.active_model)
)

const isVisionModel = computed(() => currentModel.value?.category === 'vision')

const hasWhisperModel = computed(() =>
  mlxData.value?.models?.some(m => m.category === 'speech' && m.downloaded)
)

// Auto-scroll to bottom
function scrollToBottom() {
  nextTick(() => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

// Switch model
async function switchModel(modelId: string) {
  if (modelId === mlxData.value?.active_model) {
    showModelSelector.value = false
    return
  }

  showModelSelector.value = false
  switchingModel.value = true

  try {
    // Stop current model
    await $api('/mlx/stop', { method: 'POST' })

    // Start new model
    await $api('/mlx/run', { method: 'POST', body: { model: modelId } })

    await refreshStatus()

    toast.add({
      title: 'Model switched',
      description: `Now using ${modelId.split('/').pop()}`,
      color: 'success'
    })
  } catch (error: any) {
    toast.add({
      title: 'Failed to switch model',
      description: error?.data?.error || 'Unknown error',
      color: 'error'
    })
  } finally {
    switchingModel.value = false
  }
}

// Image handling
async function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = reject
    reader.readAsDataURL(file)
  })
}

// Resize image for quick analysis (max 512px dimension)
async function resizeImage(dataUrl: string, maxSize: number = 512): Promise<string> {
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => {
      const canvas = document.createElement('canvas')
      let { width, height } = img

      // Scale down if larger than maxSize
      if (width > maxSize || height > maxSize) {
        if (width > height) {
          height = (height / width) * maxSize
          width = maxSize
        } else {
          width = (width / height) * maxSize
          height = maxSize
        }
      }

      canvas.width = width
      canvas.height = height
      const ctx = canvas.getContext('2d')
      ctx?.drawImage(img, 0, 0, width, height)
      resolve(canvas.toDataURL('image/jpeg', 0.8))
    }
    img.src = dataUrl
  })
}

async function addImage(file: File) {
  if (uploadedImages.value.length >= maxImages) return
  if (!file.type.startsWith('image/')) return

  const preview = URL.createObjectURL(file)
  const fullUrl = await fileToDataUrl(file)
  const url = await resizeImage(fullUrl, 512) // Quick analysis uses resized
  uploadedImages.value.push({ url, preview, fullUrl })
}

function removeImage(index: number) {
  const removed = uploadedImages.value.splice(index, 1)
  if (removed[0]?.preview) {
    URL.revokeObjectURL(removed[0].preview)
  }
}

function handleFileSelect(event: Event) {
  const files = (event.target as HTMLInputElement).files
  if (files?.length) {
    for (const file of files) {
      addImage(file)
    }
  }
  if (fileInputRef.value) fileInputRef.value.value = ''
}

function handleDrop(event: DragEvent) {
  event.preventDefault()
  isDragging.value = false
  const files = event.dataTransfer?.files
  if (files?.length) {
    for (const file of files) {
      addImage(file)
    }
  }
}

function handlePaste(event: ClipboardEvent) {
  if (!isVisionModel.value) return
  const items = event.clipboardData?.items
  if (!items) return

  for (const item of items) {
    if (item.type.startsWith('image/')) {
      const file = item.getAsFile()
      if (file) addImage(file)
    }
  }
}

// Voice recording
async function startRecording() {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    const recorder = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' })

    recorder.ondataavailable = (e) => {
      if (e.data.size > 0) audioChunks.value.push(e.data)
    }

    recorder.onstop = async () => {
      stream.getTracks().forEach(track => track.stop())
      await transcribeAudio()
    }

    mediaRecorder.value = recorder
    audioChunks.value = []
    recorder.start(100)
    isRecording.value = true
  } catch (error) {
    toast.add({ title: 'Error', description: 'Microphone access denied', color: 'error' })
  }
}

function stopRecording() {
  if (mediaRecorder.value?.state === 'recording') {
    mediaRecorder.value.stop()
  }
  isRecording.value = false
}

async function transcribeAudio() {
  if (!audioChunks.value.length) return

  transcribing.value = true

  try {
    const audioBlob = new Blob(audioChunks.value, { type: 'audio/webm' })
    const formData = new FormData()
    formData.append('file', audioBlob, 'audio.webm')

    const response = await fetch('/api/mlx/transcribe', {
      method: 'POST',
      body: formData,
      credentials: 'include'
    })

    if (!response.ok) throw new Error('Transcription failed')

    const data = await response.json()
    if (data.text) {
      input.value = (input.value + ' ' + data.text).trim()
    }
  } catch (error) {
    toast.add({ title: 'Error', description: 'Transcription failed', color: 'error' })
  } finally {
    transcribing.value = false
  }
}

// Start loading timer
function startLoadingTimer(hasImages: boolean) {
  loadingDuration.value = 0
  loadingStatus.value = hasImages ? 'Processing image...' : 'Thinking...'

  loadingTimer = setInterval(() => {
    loadingDuration.value++

    // Update status message based on duration
    if (hasImages) {
      if (loadingDuration.value < 5) {
        loadingStatus.value = 'Processing image...'
      } else if (loadingDuration.value < 15) {
        loadingStatus.value = 'Analyzing image content...'
      } else if (loadingDuration.value < 30) {
        loadingStatus.value = 'Generating response...'
      } else {
        loadingStatus.value = `Still processing... (${loadingDuration.value}s)`
      }
    } else {
      if (loadingDuration.value < 3) {
        loadingStatus.value = 'Thinking...'
      } else if (loadingDuration.value < 10) {
        loadingStatus.value = 'Generating response...'
      } else {
        loadingStatus.value = `Still working... (${loadingDuration.value}s)`
      }
    }
  }, 1000)
}

// Stop loading timer
function stopLoadingTimer() {
  if (loadingTimer) {
    clearInterval(loadingTimer)
    loadingTimer = null
  }
  loadingStatus.value = ''
  loadingDuration.value = 0
}

// Store full resolution images for deep analysis
const pendingFullImages = ref<string[]>([])

// Send message
async function sendMessage(deepAnalysis = false) {
  if (!input.value.trim() && !uploadedImages.value.length && !deepAnalysis) return
  if (loading.value || !mlxData.value?.running) return

  const userMessage = input.value.trim()
  const hasImages = uploadedImages.value.length > 0 && isVisionModel.value

  // Build message content
  let messageContent: string | MessageContent[]
  let maxTokens = 2048

  if (hasImages) {
    // For quick analysis: use resized images, shorter response
    // For deep analysis: use full images, longer response
    const imageUrls = deepAnalysis
      ? uploadedImages.value.map(img => img.fullUrl || img.url)
      : uploadedImages.value.map(img => img.url)

    messageContent = [
      ...imageUrls.map(url => ({
        type: 'image_url' as const,
        image_url: { url }
      })),
      { type: 'text' as const, text: userMessage || (deepAnalysis ? 'Analyze this image in detail.' : 'Briefly describe what you see in this image.') }
    ]

    // Quick = short response, Deep = full response
    maxTokens = deepAnalysis ? 2048 : 200

    // Store full images for potential deep analysis later
    if (!deepAnalysis) {
      pendingFullImages.value = uploadedImages.value.map(img => img.fullUrl || img.url)
    }
  } else {
    messageContent = userMessage
  }

  // Get text content for saving to DB (don't save base64 images)
  const textContent = hasImages
    ? userMessage || (deepAnalysis ? '[deep image analysis]' : '[quick image analysis]')
    : userMessage

  messages.value.push({ role: 'user', content: messageContent })
  const userMsgIndex = messages.value.length - 1
  input.value = ''
  uploadedImages.value = []
  loading.value = true
  startLoadingTimer(hasImages)
  scrollToBottom()

  // Track if this was an image message for "analyze deeper" button
  if (hasImages && !deepAnalysis) {
    lastImageMessageIndex.value = userMsgIndex
  }

  // Save user message to database
  await saveMessageToDb('user', textContent)

  try {
    const controller = new AbortController()
    const timeoutMs = deepAnalysis ? 180000 : 60000 // 3min for deep, 1min for quick
    const timeoutId = setTimeout(() => controller.abort(), timeoutMs)

    const response = await fetch(mlxData.value.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        model: mlxData.value.active_model,
        messages: messages.value.map(m => ({ role: m.role, content: m.content })),
        max_tokens: maxTokens
      }),
      signal: controller.signal
    })

    clearTimeout(timeoutId)

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.error?.message || `HTTP ${response.status}`)
    }

    const data = await response.json()
    const assistantMessage = data.choices?.[0]?.message?.content || 'No response'
    messages.value.push({ role: 'assistant', content: assistantMessage })

    // Save assistant message to database
    await saveMessageToDb('assistant', assistantMessage)

    scrollToBottom()
  } catch (error) {
    let errorMsg = 'Failed to get response'
    if (error instanceof Error) {
      if (error.name === 'AbortError') {
        errorMsg = 'Request timed out. The model may be overloaded or the image is too complex.'
      } else {
        errorMsg = error.message
      }
    }
    toast.add({
      title: 'Error',
      description: errorMsg,
      color: 'error'
    })
    // Remove the last user message on error so they can retry
    messages.value.pop()
  } finally {
    loading.value = false
    stopLoadingTimer()
  }
}

// Clear chat
async function clearChat() {
  messages.value = []
  uploadedImages.value.forEach(img => URL.revokeObjectURL(img.preview))
  uploadedImages.value = []
  pendingFullImages.value = []
  lastImageMessageIndex.value = null

  // Clear from database
  if (mlxData.value?.active_model) {
    try {
      await $api(`/chat/messages/${encodeURIComponent(mlxData.value.active_model)}`, {
        method: 'DELETE'
      })
    } catch {
      // Ignore errors
    }
  }
}

// Check if we can offer deep analysis (last response was a quick image analysis)
const canAnalyzeDeeper = computed(() => {
  return lastImageMessageIndex.value !== null &&
    pendingFullImages.value.length > 0 &&
    !loading.value
})

// Perform deep analysis on the last image
async function analyzeDeeper() {
  if (!pendingFullImages.value.length || loading.value) return

  // Rebuild uploadedImages with full URLs for deep analysis
  uploadedImages.value = pendingFullImages.value.map(url => ({
    url,
    preview: '',
    fullUrl: url
  }))

  input.value = 'Now analyze this image in detail. Describe everything you see.'
  lastImageMessageIndex.value = null
  pendingFullImages.value = []

  await sendMessage(true)
}

// Get text from message content
function getMessageText(content: string | MessageContent[]): string {
  if (typeof content === 'string') return content
  const textItem = content.find(c => c.type === 'text') as TextContent | undefined
  return textItem?.text || ''
}

// Get images from message content
function getMessageImages(content: string | MessageContent[]): string[] {
  if (typeof content === 'string') return []
  return content
    .filter((c): c is ImageContent => c.type === 'image_url')
    .map(c => c.image_url.url)
}

// Setup paste listener (no auto-refresh - use manual refresh button)
onMounted(() => {
  document.addEventListener('paste', handlePaste)
})
onUnmounted(() => {
  if (loadingTimer) clearInterval(loadingTimer)
  document.removeEventListener('paste', handlePaste)
  uploadedImages.value.forEach(img => URL.revokeObjectURL(img.preview))
})

// Manual refresh
const refreshing = ref(false)
async function manualRefresh() {
  refreshing.value = true
  await refreshStatus()
  refreshing.value = false
}
</script>

<template>
  <div class="h-[calc(100vh-8rem)] flex flex-col relative">
    <!-- Switching Model Overlay -->
    <div v-if="switchingModel" class="absolute inset-0 bg-white/80 dark:bg-gray-900/80 flex items-center justify-center z-20 rounded-lg">
      <div class="text-center">
        <UIcon name="i-heroicons-arrow-path" class="w-8 h-8 animate-spin text-primary-500 mb-2" />
        <p class="text-gray-600 dark:text-gray-400">Switching model...</p>
      </div>
    </div>

    <!-- Header -->
    <div class="flex items-center justify-between pb-4 border-b border-gray-200 dark:border-gray-700">
      <div class="flex items-center gap-3">
        <NuxtLink to="/llms" class="text-gray-500 hover:text-gray-700 dark:hover:text-gray-300">
          <UIcon name="i-heroicons-arrow-left" class="w-5 h-5" />
        </NuxtLink>
        <div>
          <h2 class="text-lg font-semibold">Chat</h2>
          <!-- Model Selector -->
          <UPopover v-if="mlxData?.running && downloadedModels.length > 1" v-model:open="showModelSelector">
            <template #default>
              <button class="flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors">
                <span class="w-2 h-2 bg-green-500 rounded-full" />
                {{ modelName }}
                <UIcon name="i-heroicons-chevron-down" class="w-3 h-3" />
              </button>
            </template>
            <div class="p-2 w-72 max-h-80 overflow-y-auto">
              <p class="text-xs text-gray-500 px-2 pb-2">Switch model</p>
              <div v-for="model in downloadedModels" :key="model.id"
                :class="[
                  'p-2 rounded cursor-pointer transition-colors',
                  model.id === mlxData?.active_model
                    ? 'bg-primary-100 dark:bg-primary-900/30'
                    : 'hover:bg-gray-100 dark:hover:bg-gray-800'
                ]"
                @click="switchModel(model.id)">
                <div class="flex items-center justify-between">
                  <span class="font-medium text-sm">{{ model.name }}</span>
                  <div class="flex items-center gap-2">
                    <UBadge v-if="model.category === 'vision'" color="secondary" size="xs">Vision</UBadge>
                    <UBadge v-if="model.category === 'speech'" color="info" size="xs">Speech</UBadge>
                    <UIcon v-if="model.id === mlxData?.active_model"
                      name="i-heroicons-check" class="w-4 h-4 text-green-500" />
                  </div>
                </div>
                <span class="text-xs text-gray-500">{{ model.size }}</span>
              </div>
            </div>
          </UPopover>
          <!-- Single model display -->
          <p v-else-if="mlxData?.running" class="text-sm text-gray-500">
            <span class="inline-flex items-center gap-1">
              <span class="w-2 h-2 bg-green-500 rounded-full" />
              {{ modelName }}
            </span>
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <UBadge v-if="isVisionModel" color="secondary" variant="soft" size="sm">
          <UIcon name="i-heroicons-eye" class="w-3 h-3 mr-1" />
          Vision
        </UBadge>
        <UButton variant="ghost" color="neutral" size="sm" :loading="refreshing" @click="manualRefresh">
          <UIcon name="i-heroicons-arrow-path" class="w-4 h-4" />
        </UButton>
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
            <p v-if="isVisionModel" class="text-xs mt-2 text-purple-500">
              <UIcon name="i-heroicons-photo" class="w-4 h-4 inline" />
              This model supports image input
            </p>
          </div>
        </div>

        <div
          v-for="(msg, idx) in messages"
          :key="idx"
          :class="msg.role === 'user' ? 'flex justify-end' : 'flex justify-start'"
        >
          <div
            :class="[
              'max-w-[80%] rounded-2xl overflow-hidden',
              msg.role === 'user'
                ? 'bg-primary-500 text-white rounded-br-md'
                : 'bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-bl-md'
            ]"
          >
            <!-- Images in message -->
            <div v-if="getMessageImages(msg.content).length" class="p-2 flex flex-wrap gap-2">
              <img
                v-for="(imgUrl, i) in getMessageImages(msg.content)"
                :key="i"
                :src="imgUrl"
                class="max-w-[150px] max-h-[150px] rounded-lg object-cover"
              />
            </div>
            <!-- Text content -->
            <p class="px-4 py-3 whitespace-pre-wrap">{{ getMessageText(msg.content) }}</p>
          </div>
        </div>

        <!-- Loading indicator with status -->
        <div v-if="loading" class="flex justify-start">
          <div class="bg-gray-100 dark:bg-gray-800 px-4 py-3 rounded-2xl rounded-bl-md">
            <div class="flex items-center gap-2">
              <div class="flex items-center gap-1">
                <span class="w-2 h-2 bg-primary-500 rounded-full animate-bounce" style="animation-delay: 0ms" />
                <span class="w-2 h-2 bg-primary-500 rounded-full animate-bounce" style="animation-delay: 150ms" />
                <span class="w-2 h-2 bg-primary-500 rounded-full animate-bounce" style="animation-delay: 300ms" />
              </div>
              <span class="text-sm text-gray-500">{{ loadingStatus }}</span>
            </div>
          </div>
        </div>

        <!-- Analyze Deeper button after quick image analysis -->
        <div v-if="canAnalyzeDeeper" class="flex justify-center">
          <UButton
            variant="soft"
            color="secondary"
            size="sm"
            @click="analyzeDeeper"
          >
            <UIcon name="i-heroicons-magnifying-glass-plus" class="w-4 h-4 mr-1" />
            Analyze Deeper
            <span class="text-xs opacity-70 ml-1">(may take 1-2 min)</span>
          </UButton>
        </div>
      </div>

      <!-- Input area -->
      <div class="pt-4 border-t border-gray-200 dark:border-gray-700">
        <!-- Image upload area (only for vision models) -->
        <div v-if="isVisionModel" class="mb-3">
          <!-- Image Previews -->
          <div v-if="uploadedImages.length" class="flex flex-wrap gap-2 mb-2">
            <div v-for="(img, idx) in uploadedImages" :key="idx"
              class="relative w-20 h-20 rounded-lg overflow-hidden border border-gray-200 dark:border-gray-700">
              <img :src="img.preview" class="w-full h-full object-cover" />
              <button @click="removeImage(idx)"
                class="absolute top-1 right-1 p-1 bg-black/50 rounded-full hover:bg-black/70 transition-colors">
                <UIcon name="i-heroicons-x-mark" class="w-3 h-3 text-white" />
              </button>
            </div>
          </div>

          <!-- Drop Zone -->
          <div
            v-if="uploadedImages.length < maxImages"
            :class="[
              'border-2 border-dashed rounded-lg p-3 text-center transition-colors cursor-pointer',
              isDragging ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-300 dark:border-gray-600',
              'hover:border-primary-400'
            ]"
            @click="fileInputRef?.click()"
            @dragover.prevent="isDragging = true"
            @dragleave="isDragging = false"
            @drop="handleDrop"
          >
            <input ref="fileInputRef" type="file" accept="image/*" multiple hidden @change="handleFileSelect" />
            <UIcon name="i-heroicons-photo" class="w-6 h-6 mx-auto text-gray-400" />
            <p class="text-xs text-gray-500 mt-1">Drop images, paste, or click to upload</p>
          </div>
        </div>

        <!-- Input form -->
        <form @submit.prevent="() => sendMessage()" class="flex gap-2 items-end">
          <!-- Voice input button -->
          <div v-if="hasWhisperModel">
            <UButton
              v-if="isRecording"
              icon="i-heroicons-stop"
              color="error"
              variant="soft"
              @click="stopRecording"
            />
            <UButton
              v-else-if="transcribing"
              icon="i-heroicons-arrow-path"
              variant="ghost"
              color="neutral"
              loading
            />
            <UButton
              v-else
              icon="i-heroicons-microphone"
              variant="ghost"
              color="neutral"
              :disabled="loading"
              @click="startRecording"
            />
          </div>
          <UTooltip v-else-if="!hasWhisperModel" text="Download a Whisper model for voice input">
            <UButton
              icon="i-heroicons-microphone"
              variant="ghost"
              color="neutral"
              disabled
            />
          </UTooltip>

          <UTextarea
            v-model="input"
            :placeholder="isVisionModel ? 'Describe the image or ask a question...' : 'Type a message...'"
            class="flex-1"
            :rows="1"
            autoresize
            :maxrows="5"
            :disabled="loading"
            @keydown.enter.exact.prevent="sendMessage"
          />

          <!-- Image upload button (only for vision models) -->
          <UButton
            v-if="isVisionModel"
            icon="i-heroicons-photo"
            variant="ghost"
            color="neutral"
            :disabled="loading || uploadedImages.length >= maxImages"
            @click="fileInputRef?.click()"
          />

          <UButton type="submit" size="lg" :loading="loading"
            :disabled="!input.trim() && !uploadedImages.length">
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
