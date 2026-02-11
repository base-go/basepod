<script setup lang="ts">
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  appId: string
}>()

const terminalRef = ref<HTMLElement | null>(null)
const status = ref<'connecting' | 'connected' | 'disconnected' | 'error'>('disconnected')
const errorMsg = ref('')

let terminal: Terminal | null = null
let fitAddon: FitAddon | null = null
let ws: WebSocket | null = null
let resizeObserver: ResizeObserver | null = null

function getWsUrl() {
  const config = useRuntimeConfig()
  const base = config.public.apiBase as string
  // Convert http(s) to ws(s)
  const wsBase = base.replace(/^http/, 'ws')
  return `${wsBase}/apps/${props.appId}/terminal`
}

function connect() {
  if (ws) {
    ws.close()
    ws = null
  }

  status.value = 'connecting'
  errorMsg.value = ''

  const url = getWsUrl()
  ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'

  ws.onopen = () => {
    status.value = 'connected'
    terminal?.clear()
    // Send initial resize
    if (terminal) {
      ws?.send(`resize:${terminal.cols},${terminal.rows}`)
    }
  }

  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      terminal?.write(new Uint8Array(event.data))
    } else {
      terminal?.write(event.data)
    }
  }

  ws.onclose = () => {
    if (status.value === 'connected') {
      status.value = 'disconnected'
    }
  }

  ws.onerror = () => {
    status.value = 'error'
    errorMsg.value = 'Connection failed'
  }
}

onMounted(() => {
  if (!terminalRef.value) return

  terminal = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    theme: {
      background: '#0a0a0a',
      foreground: '#d4d4d4',
      cursor: '#d4d4d4',
      cursorAccent: '#0a0a0a',
      selectionBackground: '#264f78',
      black: '#000000',
      red: '#f44747',
      green: '#6a9955',
      yellow: '#d7ba7d',
      blue: '#569cd6',
      magenta: '#c586c0',
      cyan: '#4ec9b0',
      white: '#d4d4d4',
    },
  })

  fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.open(terminalRef.value)
  fitAddon.fit()

  // Handle terminal input → WebSocket
  terminal.onData((data) => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data))
    }
  })

  // Handle resize
  terminal.onResize(({ cols, rows }) => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(`resize:${cols},${rows}`)
    }
  })

  // Observe container size changes
  resizeObserver = new ResizeObserver(() => {
    fitAddon?.fit()
  })
  resizeObserver.observe(terminalRef.value)

  connect()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  ws?.close()
  terminal?.dispose()
})
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Status bar -->
    <div class="flex items-center justify-between px-4 py-2 bg-gray-950 border-b border-gray-800 rounded-t-lg">
      <div class="flex items-center gap-2">
        <span
          class="w-2 h-2 rounded-full"
          :class="{
            'bg-green-500': status === 'connected',
            'bg-yellow-500 animate-pulse': status === 'connecting',
            'bg-gray-500': status === 'disconnected',
            'bg-red-500': status === 'error',
          }"
        />
        <span class="text-sm text-gray-400">
          {{ status === 'connected' ? 'Connected' : status === 'connecting' ? 'Connecting...' : status === 'error' ? errorMsg : 'Disconnected' }}
        </span>
      </div>
      <UButton
        v-if="status !== 'connecting'"
        size="xs"
        variant="ghost"
        color="neutral"
        icon="i-heroicons-arrow-path"
        @click="connect"
      >
        Reconnect
      </UButton>
    </div>

    <!-- Terminal — fill remaining viewport -->
    <div ref="terminalRef" class="flex-1 bg-[#0a0a0a] rounded-b-lg" style="min-height: calc(100vh - 320px);" />
  </div>
</template>
