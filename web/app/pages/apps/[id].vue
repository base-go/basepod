<script setup lang="ts">
import type { App, HealthCheckConfig, AppHealthStatus, WebhookDelivery, WebhookSetupResponse } from '~/types'
import Convert from 'ansi-to-html'

const route = useRoute()
const toast = useToast()
const ansiConvert = new Convert({ fg: '#c0caf5', bg: '#1a1b26', newline: true })
const appId = route.params.id as string

const { data: app, refresh } = await useApiFetch<App>(`/apps/${appId}`)

const activeTab = ref('overview')

// Tabs vary by app type - static sites don't have volumes or env
const tabs = computed(() => {
  const baseTabs = [
    { label: 'Overview', value: 'overview', icon: 'i-heroicons-information-circle' },
    { label: 'Deployments', value: 'deployments', icon: 'i-heroicons-rocket-launch' },
    { label: 'Logs', value: 'logs', icon: 'i-heroicons-document-text' },
  ]

  // Only show container-specific tabs for non-static apps
  if (app.value?.type !== 'static') {
    baseTabs.push(
      { label: 'Health', value: 'health', icon: 'i-heroicons-heart' },
      { label: 'Terminal', value: 'terminal', icon: 'i-heroicons-command-line' },
      { label: 'Volumes', value: 'volumes', icon: 'i-lucide-hard-drive' },
      { label: 'Environment', value: 'env', icon: 'i-heroicons-key' },
    )
  }

  baseTabs.push({ label: 'Settings', value: 'settings', icon: 'i-heroicons-cog-6-tooth' })
  return baseTabs
})

const logs = ref('')
const accessLogs = ref<any[]>([])
const logsLoading = ref(false)

async function fetchLogs() {
  logsLoading.value = true
  try {
    if (app.value?.type === 'static') {
      const data = await $api<{ logs: any[], total: number }>(`/apps/${appId}/access-logs?limit=100`)
      accessLogs.value = data.logs || []
    } else {
      if (!app.value?.container_id) return
      logs.value = await $api<string>(`/apps/${appId}/logs?tail=100`)
    }
  } catch {
    logs.value = ''
    accessLogs.value = []
  } finally {
    logsLoading.value = false
  }
}

function formatLogTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  return d.toLocaleString()
}

function getStatusColor(status: number): "error" | "primary" | "secondary" | "success" | "info" | "warning" | "neutral" {
  if (status < 300) return 'success'
  if (status < 400) return 'warning'
  return 'error'
}

const logsHtml = computed(() => {
  if (!logs.value) return ''
  return ansiConvert.toHtml(logs.value)
})

watch(activeTab, (tab) => {
  if (tab === 'logs') {
    fetchLogs()
  }
})

function getErrorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'data' in error) {
    const data = (error as { data?: { error?: string } }).data
    if (data?.error) return data.error
  }
  return 'An unexpected error occurred'
}

async function startApp() {
  try {
    await $api(`/apps/${appId}/start`, { method: 'POST' })
    toast.add({ title: 'App started', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to start', description: getErrorMessage(error), color: 'error' })
  }
}

async function stopApp() {
  try {
    await $api(`/apps/${appId}/stop`, { method: 'POST' })
    toast.add({ title: 'App stopped', color: 'warning' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to stop', description: getErrorMessage(error), color: 'error' })
  }
}

async function restartApp() {
  try {
    await $api(`/apps/${appId}/restart`, { method: 'POST' })
    toast.add({ title: 'App restarted', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to restart', description: getErrorMessage(error), color: 'error' })
  }
}

// Settings form
const settingsForm = ref({
  name: '',
  domain: '',
  image: '',
  port: 8080,
  memory: 0,
  cpus: 0,
  exposeExternal: false
})

// Domain aliases management
const aliases = ref<string[]>([])
const newAlias = ref('')
const savingAliases = ref(false)
const aliasesInitialized = ref(false)
const savingSettings = ref(false)

// Environment variables management
const envVars = ref<Array<{ key: string; value: string }>>([])
const savingEnv = ref(false)
const envInitialized = ref(false)

// Initialize env vars when app data loads (only once)
watch(() => app.value, (appData) => {
  if (appData?.env && !envInitialized.value) {
    envVars.value = Object.entries(appData.env).map(([key, value]) => ({ key, value }))
    envInitialized.value = true
  }
}, { immediate: true })

// Database credentials detection
const dbCredentials = computed(() => {
  if (!app.value?.env) return null
  const env = app.value.env

  // PostgreSQL
  if (env.POSTGRES_PASSWORD) {
    return {
      type: 'PostgreSQL',
      username: env.POSTGRES_USER || 'postgres',
      password: env.POSTGRES_PASSWORD,
      database: env.POSTGRES_DB || 'postgres'
    }
  }
  // MySQL
  if (env.MYSQL_ROOT_PASSWORD) {
    return {
      type: 'MySQL',
      username: 'root',
      password: env.MYSQL_ROOT_PASSWORD,
      database: env.MYSQL_DATABASE || ''
    }
  }
  // MariaDB
  if (env.MARIADB_ROOT_PASSWORD) {
    return {
      type: 'MariaDB',
      username: 'root',
      password: env.MARIADB_ROOT_PASSWORD,
      database: env.MARIADB_DATABASE || ''
    }
  }
  // MongoDB
  if (env.MONGO_INITDB_ROOT_PASSWORD) {
    return {
      type: 'MongoDB',
      username: env.MONGO_INITDB_ROOT_USERNAME || 'admin',
      password: env.MONGO_INITDB_ROOT_PASSWORD,
      database: ''
    }
  }
  // Redis (no auth by default, but check for password)
  if (app.value?.image?.includes('redis')) {
    return {
      type: 'Redis',
      username: '',
      password: env.REDIS_PASSWORD || '',
      database: ''
    }
  }
  return null
})

function addEmptyEnvVar() {
  envVars.value.push({ key: '', value: '' })
  // Focus the new input after Vue updates the DOM
  nextTick(() => {
    const inputs = document.querySelectorAll('input[placeholder="KEY"]')
    const lastInput = inputs[inputs.length - 1] as HTMLInputElement
    lastInput?.focus()
  })
}

function removeEnvVar(index: number) {
  envVars.value.splice(index, 1)
}

async function saveEnvVars() {
  savingEnv.value = true
  try {
    const envObject: Record<string, string> = {}
    for (const { key, value } of envVars.value) {
      if (key.trim()) {
        envObject[key.trim()] = value
      }
    }
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: { env: envObject }
    })
    toast.add({ title: 'Environment variables saved', color: 'success' })
    // Sync local state with what we just saved
    envVars.value = Object.entries(envObject).map(([key, value]) => ({ key, value }))
  } catch (error) {
    toast.add({ title: 'Failed to save', description: getErrorMessage(error), color: 'error' })
  } finally {
    savingEnv.value = false
  }
}

// Volumes management
const volumeList = ref<Array<{ name: string; container_path: string; read_only: boolean; host_path?: string }>>([])
const savingVolumes = ref(false)
const volumesInitialized = ref(false)

// Initialize volumes when app data loads (only once)
watch(() => app.value, (appData) => {
  if (appData?.volumes && !volumesInitialized.value) {
    volumeList.value = appData.volumes.map((v: any) => ({
      name: v.name || '',
      container_path: v.container_path || '',
      read_only: v.read_only || false,
      host_path: v.host_path || ''
    }))
    volumesInitialized.value = true
  }
}, { immediate: true })

function addVolume() {
  volumeList.value.push({ name: '', container_path: '', read_only: false })
  nextTick(() => {
    const inputs = document.querySelectorAll('input[placeholder="volume-name"]')
    const lastInput = inputs[inputs.length - 1] as HTMLInputElement
    lastInput?.focus()
  })
}

function removeVolume(index: number) {
  volumeList.value.splice(index, 1)
}

async function saveVolumes() {
  savingVolumes.value = true
  try {
    const volumes = volumeList.value
      .filter(v => v.name.trim() && v.container_path.trim())
      .map(v => ({
        name: v.name.trim(),
        container_path: v.container_path.trim(),
        read_only: v.read_only
      }))
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: { volumes }
    })
    toast.add({ title: 'Volumes saved', color: 'success' })
    volumeList.value = volumes.map(v => ({ ...v, host_path: '' }))
  } catch (error) {
    toast.add({ title: 'Failed to save volumes', description: getErrorMessage(error), color: 'error' })
  } finally {
    savingVolumes.value = false
  }
}

// Initialize settings form when app data loads
watch(() => app.value, (appData) => {
  if (appData) {
    settingsForm.value = {
      name: appData.name || '',
      domain: appData.domain || '',
      image: appData.image || '',
      port: appData.ports?.container_port || 8080,
      memory: appData.resources?.memory || 0,
      cpus: appData.resources?.cpus || 0,
      exposeExternal: appData.ports?.expose_external || false
    }
    // Initialize aliases (only once)
    if (!aliasesInitialized.value) {
      aliases.value = appData.aliases || []
      aliasesInitialized.value = true
    }
  }
}, { immediate: true })

function addAlias() {
  const alias = newAlias.value.trim().toLowerCase()
  if (alias && !aliases.value.includes(alias)) {
    aliases.value.push(alias)
    newAlias.value = ''
  }
}

function removeAlias(index: number) {
  aliases.value.splice(index, 1)
}

async function saveAliases() {
  savingAliases.value = true
  try {
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: { aliases: aliases.value }
    })
    toast.add({ title: 'Domain aliases saved', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to save aliases', description: getErrorMessage(error), color: 'error' })
  } finally {
    savingAliases.value = false
  }
}

async function saveSettings() {
  savingSettings.value = true
  const exposeExternalChanged = settingsForm.value.exposeExternal !== (app.value?.ports?.expose_external || false)
  try {
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: {
        name: settingsForm.value.name,
        domain: settingsForm.value.domain,
        image: settingsForm.value.image || null,
        port: settingsForm.value.port,
        memory: settingsForm.value.memory || null,
        cpus: settingsForm.value.cpus || null,
        expose_external: settingsForm.value.exposeExternal
      }
    })
    toast.add({ title: 'Settings saved', color: 'success' })
    refresh()
    // Show restart reminder if external access setting changed
    if (exposeExternalChanged) {
      showRestartModal.value = true
    }
  } catch (error) {
    toast.add({ title: 'Failed to save', description: getErrorMessage(error), color: 'error' })
  } finally {
    savingSettings.value = false
  }
}

// Health check management
const healthStatus = ref<AppHealthStatus | null>(null)
const healthLoading = ref(false)
const healthCheckEnabled = ref(false)
const healthForm = ref<HealthCheckConfig>({
  endpoint: '/health',
  interval: 30,
  timeout: 5,
  max_failures: 3,
  auto_restart: true,
})
const savingHealth = ref(false)
const healthInitialized = ref(false)
let healthInterval: ReturnType<typeof setInterval> | null = null

// Initialize health config when app data loads
watch(() => app.value, (appData) => {
  if (appData && !healthInitialized.value) {
    healthCheckEnabled.value = !!appData.health_check
    if (appData.health_check) {
      healthForm.value = { ...appData.health_check }
    }
    if (appData.health) {
      healthStatus.value = appData.health
    }
    healthInitialized.value = true
  }
}, { immediate: true })

// Auto-refresh health status when tab is active
watch(activeTab, (tab) => {
  if (tab === 'health') {
    fetchHealthStatus()
    healthInterval = setInterval(fetchHealthStatus, 10000)
  } else {
    if (healthInterval) {
      clearInterval(healthInterval)
      healthInterval = null
    }
  }
})

onUnmounted(() => {
  if (healthInterval) {
    clearInterval(healthInterval)
  }
})

async function fetchHealthStatus() {
  if (!app.value) return
  healthLoading.value = true
  try {
    healthStatus.value = await $api<AppHealthStatus>(`/apps/${appId}/health`)
  } catch {
    // Health endpoint may not be available yet
  } finally {
    healthLoading.value = false
  }
}

async function triggerHealthCheck() {
  healthLoading.value = true
  try {
    healthStatus.value = await $api<AppHealthStatus>(`/apps/${appId}/health/check`, { method: 'POST' })
    toast.add({ title: 'Health check triggered', color: 'success' })
  } catch (error) {
    toast.add({ title: 'Health check failed', description: getErrorMessage(error), color: 'error' })
  } finally {
    healthLoading.value = false
  }
}

async function saveHealthConfig() {
  savingHealth.value = true
  try {
    const healthCheck = healthCheckEnabled.value ? healthForm.value : {
      endpoint: '',
      interval: 0,
      timeout: 0,
      max_failures: 0,
      auto_restart: false,
    }
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: { health_check: healthCheckEnabled.value ? healthCheck : null }
    })
    toast.add({ title: 'Health check configuration saved', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to save', description: getErrorMessage(error), color: 'error' })
  } finally {
    savingHealth.value = false
  }
}

function getHealthColor(status?: string): "success" | "error" | "neutral" {
  if (status === 'healthy') return 'success'
  if (status === 'unhealthy') return 'error'
  return 'neutral'
}

function formatTime(ts?: string): string {
  if (!ts || ts === '0001-01-01T00:00:00Z') return 'Never'
  return new Date(ts).toLocaleString()
}

// Webhook management
const webhookGitUrl = ref('')
const webhookSetupLoading = ref(false)
const webhookDeliveries = ref<WebhookDelivery[]>([])
const webhookDeliveriesLoading = ref(false)
const showWebhookSecret = ref(false)
const webhookInitialized = ref(false)
let webhookInterval: ReturnType<typeof setInterval> | null = null

// Initialize webhook form when app data loads
watch(() => app.value, (appData) => {
  if (appData && !webhookInitialized.value) {
    webhookGitUrl.value = appData.deployment?.git_url || ''
    webhookInitialized.value = true
  }
}, { immediate: true })

// Auto-refresh webhook deliveries when deployments tab is active
watch(activeTab, (tab) => {
  if (tab === 'deployments') {
    fetchWebhookDeliveries()
    webhookInterval = setInterval(fetchWebhookDeliveries, 10000)
  } else {
    if (webhookInterval) {
      clearInterval(webhookInterval)
      webhookInterval = null
    }
  }
})

onUnmounted(() => {
  if (webhookInterval) {
    clearInterval(webhookInterval)
  }
})

async function setupWebhook() {
  if (!webhookGitUrl.value.trim()) {
    toast.add({ title: 'Git URL is required', color: 'error' })
    return
  }
  webhookSetupLoading.value = true
  try {
    const result = await $api<WebhookSetupResponse>(`/apps/${appId}/webhook/setup`, {
      method: 'POST',
      body: { git_url: webhookGitUrl.value.trim() }
    })
    toast.add({ title: 'Webhook enabled', description: 'Copy the URL and secret to your GitHub repository settings.', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to setup webhook', description: getErrorMessage(error), color: 'error' })
  } finally {
    webhookSetupLoading.value = false
  }
}

async function disableWebhook() {
  webhookSetupLoading.value = true
  try {
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: {
        deployment: {
          ...app.value?.deployment,
          git_url: '',
          webhook_secret: '',
          auto_deploy: false,
        }
      }
    })
    webhookGitUrl.value = ''
    showWebhookSecret.value = false
    toast.add({ title: 'Webhook disabled', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to disable webhook', description: getErrorMessage(error), color: 'error' })
  } finally {
    webhookSetupLoading.value = false
  }
}

async function regenerateSecret() {
  if (!app.value?.deployment?.git_url) return
  webhookSetupLoading.value = true
  try {
    await $api<WebhookSetupResponse>(`/apps/${appId}/webhook/setup`, {
      method: 'POST',
      body: { git_url: app.value.deployment.git_url }
    })
    toast.add({ title: 'Secret regenerated', description: 'Update the secret in your GitHub repository settings.', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to regenerate secret', description: getErrorMessage(error), color: 'error' })
  } finally {
    webhookSetupLoading.value = false
  }
}

async function fetchWebhookDeliveries() {
  if (!app.value) return
  webhookDeliveriesLoading.value = true
  try {
    const data = await $api<{ deliveries: WebhookDelivery[] }>(`/apps/${appId}/webhook/deliveries`)
    webhookDeliveries.value = data.deliveries || []
  } catch {
    // Webhook deliveries endpoint may not have any data yet
  } finally {
    webhookDeliveriesLoading.value = false
  }
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text)
  toast.add({ title: 'Copied to clipboard', color: 'success' })
}

function getDeliveryStatusColor(status: string): "success" | "error" | "warning" | "neutral" {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'error'
  if (status === 'deploying') return 'warning'
  return 'neutral'
}

// Restart reminder modal (shown when external access setting changes)
const showRestartModal = ref(false)

// Delete confirmation modal
const showDeleteModal = ref(false)

async function deleteApp() {
  try {
    await $api(`/apps/${appId}`, { method: 'DELETE' })
    toast.add({ title: 'App deleted', color: 'success' })
    navigateTo('/apps')
  } catch (error) {
    toast.add({ title: 'Failed to delete', description: getErrorMessage(error), color: 'error' })
  }
}
</script>

<template>
  <div>
    <div v-if="app">
    <!-- App Header -->
    <div class="flex items-start justify-between mb-6">
      <div class="flex items-center gap-4">
        <NuxtLink to="/apps" class="text-gray-400 hover:text-gray-600">
          <UIcon name="i-heroicons-arrow-left" class="w-5 h-5" />
        </NuxtLink>
        <div class="flex items-center justify-center w-14 h-14 rounded-xl bg-primary-100 dark:bg-primary-900/20">
          <UIcon name="i-heroicons-cube" class="w-7 h-7 text-primary-500" />
        </div>
        <div>
          <h1 class="text-2xl font-bold">{{ app.name }}</h1>
          <div v-if="app.domain && !app.ports?.expose_external" class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <a :href="`https://${app.domain}`" target="_blank" class="text-gray-500 hover:text-primary-500">
              {{ app.domain }}
            </a>
            <template v-if="app.aliases && app.aliases.length > 0">
              <span v-for="alias in app.aliases" :key="alias" class="text-gray-400 text-sm">
                <a :href="`https://${alias}`" target="_blank" class="hover:text-primary-500">
                  {{ alias }}
                </a>
              </span>
            </template>
          </div>
        </div>
      </div>

      <div class="flex items-center gap-3">
        <!-- Static sites show deployed/not deployed -->
        <template v-if="app.type === 'static'">
          <UBadge :color="app.status === 'running' ? 'success' : 'neutral'" size="lg">
            {{ app.status === 'running' ? 'deployed' : 'not deployed' }}
          </UBadge>
        </template>
        <!-- Container apps show running/stopped -->
        <template v-else>
          <UBadge
            :color="app.status === 'running' ? 'success' : app.status === 'stopped' ? 'warning' : 'neutral'"
            size="lg"
          >
            {{ app.status }}
          </UBadge>

          <div class="flex gap-2">
            <!-- Show Start/Stop/Restart buttons if app has been deployed -->
            <template v-if="app.container_id">
              <UButton
                v-if="app.status !== 'running'"
                icon="i-heroicons-play"
                color="success"
                @click="startApp"
              >
                Start
              </UButton>
              <UButton
                v-if="app.status === 'running'"
                icon="i-heroicons-stop"
                color="warning"
                @click="stopApp"
              >
                Stop
              </UButton>
              <UButton
                icon="i-heroicons-arrow-path"
                variant="outline"
                @click="restartApp"
              >
                Restart
              </UButton>
            </template>
            <!-- Show deploy hint if app has not been deployed -->
            <span v-else class="text-sm text-gray-500">
              Deploy with <code class="bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded font-mono">bp deploy</code>
            </span>
          </div>
        </template>
      </div>
    </div>

    <!-- Tabs -->
    <UTabs v-model="activeTab" :items="tabs" class="mb-6" />

    <!-- Overview Tab -->
    <div v-if="activeTab === 'overview'" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <UCard>
        <template #header>
          <h3 class="font-semibold">Details</h3>
        </template>

        <dl class="space-y-3">
          <div class="flex justify-between">
            <dt class="text-gray-500">ID</dt>
            <dd class="font-mono text-sm">{{ app.id.slice(0, 8) }}...</dd>
          </div>
          <div class="flex justify-between">
            <dt class="text-gray-500">Type</dt>
            <dd class="font-mono text-sm">{{ app.type || 'container' }}</dd>
          </div>
          <!-- Container-specific details -->
          <template v-if="app.type !== 'static'">
            <div class="flex justify-between">
              <dt class="text-gray-500">Container ID</dt>
              <dd class="font-mono text-sm">{{ app.container_id ? app.container_id.slice(0, 12) + '...' : '-' }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Image</dt>
              <dd class="font-mono text-sm">{{ app.image || '-' }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Port</dt>
              <dd>{{ app.ports?.container_port || 8080 }}</dd>
            </div>
          </template>
          <div class="flex justify-between">
            <dt class="text-gray-500">Created</dt>
            <dd>{{ new Date(app.created_at).toLocaleString() }}</dd>
          </div>
          <div v-if="app.updated_at" class="flex justify-between">
            <dt class="text-gray-500">Last Deployed</dt>
            <dd>{{ new Date(app.updated_at).toLocaleString() }}</dd>
          </div>
        </dl>
      </UCard>

      <!-- Connection Info - only for container apps -->
      <UCard v-if="app.type !== 'static'">
        <template #header>
          <h3 class="font-semibold">Connection Info</h3>
        </template>

        <dl class="space-y-3">
          <div v-if="app.internal_host" class="flex justify-between items-center">
            <dt class="text-gray-500">Internal Host</dt>
            <dd class="font-mono text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{{ app.internal_host }}:{{ app.ports?.container_port || 8080 }}</dd>
          </div>
          <div v-if="app.ports?.expose_external && app.external_host" class="flex justify-between items-center">
            <dt class="text-gray-500">External Host</dt>
            <dd class="font-mono text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{{ app.external_host }}</dd>
          </div>
          <div v-if="app.ports?.expose_external && app.ports?.host_port" class="flex justify-between items-center">
            <dt class="text-gray-500">Host Port</dt>
            <dd class="font-mono text-sm">{{ app.ports.host_port }}</dd>
          </div>

          <!-- Database Credentials -->
          <template v-if="dbCredentials">
            <div class="border-t border-gray-200 dark:border-gray-700 pt-3 mt-3">
              <div class="text-xs text-gray-500 uppercase tracking-wide mb-2">{{ dbCredentials.type }} Credentials</div>
            </div>
            <div v-if="dbCredentials.username" class="flex justify-between items-center">
              <dt class="text-gray-500">Username</dt>
              <dd class="font-mono text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{{ dbCredentials.username }}</dd>
            </div>
            <div v-if="dbCredentials.password" class="flex justify-between items-center">
              <dt class="text-gray-500">Password</dt>
              <dd class="font-mono text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{{ dbCredentials.password }}</dd>
            </div>
            <div v-if="dbCredentials.database" class="flex justify-between items-center">
              <dt class="text-gray-500">Database</dt>
              <dd class="font-mono text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{{ dbCredentials.database }}</dd>
            </div>
          </template>

          <div v-if="!app.internal_host && !app.external_host && !dbCredentials" class="text-gray-500 text-sm">
            Connection info not available
          </div>
        </dl>
      </UCard>

      <UCard>
        <template #header>
          <h3 class="font-semibold">Resources</h3>
        </template>

        <dl class="space-y-3">
          <div class="flex justify-between">
            <dt class="text-gray-500">Memory Limit</dt>
            <dd>{{ app.resources?.memory ? `${app.resources.memory} MB` : 'Unlimited' }}</dd>
          </div>
          <div class="flex justify-between">
            <dt class="text-gray-500">CPU Limit</dt>
            <dd>{{ app.resources?.cpus ? `${app.resources.cpus} cores` : 'Unlimited' }}</dd>
          </div>
          <div class="flex justify-between">
            <dt class="text-gray-500">SSL</dt>
            <dd>
              <UBadge color="success">Auto (Caddy)</UBadge>
            </dd>
          </div>
        </dl>
      </UCard>
    </div>

    <!-- Deployments Tab -->
    <UCard v-if="activeTab === 'deployments'">
      <template #header>
        <h3 class="font-semibold">Deployment History</h3>
      </template>

      <div v-if="!app.deployments || app.deployments.length === 0" class="text-center py-8 text-gray-500">
        <UIcon name="i-heroicons-rocket-launch" class="w-12 h-12 mx-auto mb-2 opacity-50" />
        <p>No deployments yet</p>
        <p class="text-sm mt-1">Deploy with <code class="bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded font-mono">bp deploy</code></p>
      </div>

      <div v-else class="space-y-3">
        <div
          v-for="(deployment, index) in app.deployments"
          :key="deployment.id"
          class="p-4 rounded-lg border"
          :class="index === 0 ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-gray-50 dark:bg-gray-800/50 border-gray-200 dark:border-gray-700'"
        >
          <div class="flex items-center justify-between mb-2">
            <div class="flex items-center gap-2">
              <UBadge v-if="index === 0" color="success" size="xs">Current</UBadge>
              <span v-if="deployment.commit_hash" class="font-mono text-sm font-medium">
                {{ deployment.commit_hash }}
              </span>
              <span v-else class="text-gray-500 text-sm">No commit info</span>
            </div>
            <span class="text-sm text-gray-500">
              {{ new Date(deployment.deployed_at).toLocaleString() }}
            </span>
          </div>
          <div v-if="deployment.commit_msg" class="text-sm text-gray-600 dark:text-gray-400 truncate">
            {{ deployment.commit_msg }}
          </div>
          <div v-if="deployment.branch" class="text-xs text-gray-500 mt-1">
            <UIcon name="i-heroicons-code-bracket" class="w-3 h-3 inline" />
            {{ deployment.branch }}
          </div>
        </div>
      </div>
    </UCard>

    <!-- Webhook Deliveries (in Deployments tab) -->
    <UCard v-if="activeTab === 'deployments' && app?.deployment?.webhook_secret" class="mt-6">
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold">Webhook Deliveries</h3>
          <UButton variant="ghost" size="sm" icon="i-heroicons-arrow-path" :loading="webhookDeliveriesLoading" @click="fetchWebhookDeliveries">
            Refresh
          </UButton>
        </div>
      </template>

      <div v-if="webhookDeliveries.length === 0" class="text-center py-6 text-gray-500">
        <UIcon name="i-heroicons-inbox" class="w-10 h-10 mx-auto mb-2 opacity-50" />
        <p class="text-sm">No webhook deliveries yet</p>
        <p class="text-xs mt-1">Push to your repository to trigger a deployment</p>
      </div>

      <div v-else class="space-y-2">
        <div
          v-for="delivery in webhookDeliveries"
          :key="delivery.id"
          class="p-3 rounded-lg bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700"
        >
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <UBadge :color="getDeliveryStatusColor(delivery.status)" size="xs">
                {{ delivery.status }}
              </UBadge>
              <span class="text-sm font-medium">{{ delivery.event }}</span>
              <span v-if="delivery.commit" class="font-mono text-xs text-gray-500">{{ delivery.commit }}</span>
            </div>
            <span class="text-xs text-gray-500">{{ new Date(delivery.created_at).toLocaleString() }}</span>
          </div>
          <div v-if="delivery.message" class="text-sm text-gray-600 dark:text-gray-400 mt-1 truncate">
            {{ delivery.message }}
          </div>
          <div v-if="delivery.branch" class="text-xs text-gray-500 mt-1">
            <UIcon name="i-heroicons-code-bracket" class="w-3 h-3 inline" />
            {{ delivery.branch }}
          </div>
          <div v-if="delivery.error" class="text-xs text-red-500 mt-1 truncate">
            {{ delivery.error }}
          </div>
        </div>
      </div>
    </UCard>

    <!-- Logs Tab -->
    <UCard v-if="activeTab === 'logs'">
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold">{{ app.type === 'static' ? 'Access Logs' : 'Container Logs' }}</h3>
          <UButton variant="ghost" size="sm" icon="i-heroicons-arrow-path" :loading="logsLoading" @click="fetchLogs">
            Refresh
          </UButton>
        </div>
      </template>

      <!-- Static site access logs -->
      <div v-if="app.type === 'static'">
        <div v-if="logsLoading" class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-arrow-path" class="w-8 h-8 mx-auto mb-2 animate-spin opacity-50" />
          <p>Loading access logs...</p>
        </div>
        <div v-else-if="accessLogs.length" class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-200 dark:border-gray-700">
                <th class="text-left py-2 px-3 font-medium text-gray-500">Time</th>
                <th class="text-left py-2 px-3 font-medium text-gray-500">Method</th>
                <th class="text-left py-2 px-3 font-medium text-gray-500">Path</th>
                <th class="text-left py-2 px-3 font-medium text-gray-500">Status</th>
                <th class="text-left py-2 px-3 font-medium text-gray-500">Size</th>
                <th class="text-left py-2 px-3 font-medium text-gray-500">Client</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(entry, i) in accessLogs.slice().reverse()"
                :key="i"
                class="border-b border-gray-100 dark:border-gray-800"
              >
                <td class="py-2 px-3 text-gray-500 whitespace-nowrap text-xs">{{ formatLogTime(entry.ts) }}</td>
                <td class="py-2 px-3">
                  <UBadge variant="subtle" color="neutral" size="xs">{{ entry.request?.method }}</UBadge>
                </td>
                <td class="py-2 px-3 font-mono text-xs max-w-[300px] truncate">{{ entry.request?.uri }}</td>
                <td class="py-2 px-3">
                  <UBadge :color="getStatusColor(entry.status)" size="xs">{{ entry.status }}</UBadge>
                </td>
                <td class="py-2 px-3 text-gray-500 text-xs">{{ entry.size || '-' }}</td>
                <td class="py-2 px-3 text-gray-500 text-xs truncate max-w-[150px]">{{ entry.request?.client_ip || entry.request?.remote_ip || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-document-text" class="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>No access logs yet</p>
          <p class="text-sm mt-1">Logs will appear after the first request</p>
        </div>
      </div>

      <!-- Container logs -->
      <template v-else>
        <div v-if="!app.container_id" class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-document-text" class="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>App has not been deployed yet</p>
        </div>
        <pre v-else class="bg-[#1a1b26] text-[#c0caf5] p-4 rounded-lg overflow-x-auto text-sm font-mono overflow-y-auto" style="min-height: calc(100vh - 320px); max-height: calc(100vh - 320px);" v-html="logsHtml || 'No logs available'" />
      </template>
    </UCard>

    <!-- Health Tab -->
    <div v-if="activeTab === 'health'" class="space-y-6">
      <!-- Health Status -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Health Status</h3>
            <UButton
              variant="outline"
              size="sm"
              icon="i-heroicons-arrow-path"
              :loading="healthLoading"
              :disabled="!app.health_check"
              @click="triggerHealthCheck"
            >
              Check Now
            </UButton>
          </div>
        </template>

        <div v-if="!app.health_check" class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-heart" class="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>Health checks are not enabled</p>
          <p class="text-sm mt-1">Enable health checks below to monitor this app</p>
        </div>

        <div v-else-if="healthStatus" class="space-y-4">
          <div class="flex items-center gap-3">
            <UBadge :color="getHealthColor(healthStatus.status)" size="lg">
              {{ healthStatus.status }}
            </UBadge>
            <span class="text-sm text-gray-500">
              Last checked: {{ formatTime(healthStatus.last_check) }}
            </span>
          </div>

          <dl class="grid grid-cols-2 gap-4">
            <div>
              <dt class="text-sm text-gray-500">Last Success</dt>
              <dd class="font-medium">{{ formatTime(healthStatus.last_success) }}</dd>
            </div>
            <div>
              <dt class="text-sm text-gray-500">Consecutive Failures</dt>
              <dd class="font-medium">{{ healthStatus.consecutive_failures }}</dd>
            </div>
            <div>
              <dt class="text-sm text-gray-500">Total Checks</dt>
              <dd class="font-medium">{{ healthStatus.total_checks }}</dd>
            </div>
            <div>
              <dt class="text-sm text-gray-500">Total Failures</dt>
              <dd class="font-medium">{{ healthStatus.total_failures }}</dd>
            </div>
          </dl>

          <div v-if="healthStatus.last_error" class="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
            <div class="flex items-start gap-2">
              <UIcon name="i-heroicons-exclamation-triangle" class="w-5 h-5 text-red-500 shrink-0" />
              <span class="text-sm text-red-600 dark:text-red-400">{{ healthStatus.last_error }}</span>
            </div>
          </div>
        </div>

        <div v-else class="text-center py-4 text-gray-500">
          <p class="text-sm">No health data yet. Waiting for first check...</p>
        </div>
      </UCard>

      <!-- Health Check Configuration -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Configuration</h3>
            <UButton :loading="savingHealth" @click="saveHealthConfig">
              Save
            </UButton>
          </div>
        </template>

        <div class="space-y-4">
          <div class="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
            <div>
              <div class="font-medium text-sm">Enable Health Checks</div>
              <div class="text-xs text-gray-500">Periodically check if your app is responding</div>
            </div>
            <USwitch v-model="healthCheckEnabled" />
          </div>

          <template v-if="healthCheckEnabled">
            <UFormField label="Health Endpoint" hint="HTTP path to check">
              <UInput v-model="healthForm.endpoint" placeholder="/health" />
            </UFormField>

            <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
              <UFormField label="Check Interval (seconds)" hint="Time between checks">
                <UInput v-model.number="healthForm.interval" type="number" min="5" placeholder="30" />
              </UFormField>

              <UFormField label="Timeout (seconds)" hint="Max wait per check">
                <UInput v-model.number="healthForm.timeout" type="number" min="1" placeholder="5" />
              </UFormField>

              <UFormField label="Max Failures" hint="Before auto-restart">
                <UInput v-model.number="healthForm.max_failures" type="number" min="1" placeholder="3" />
              </UFormField>
            </div>

            <div class="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
              <div>
                <div class="font-medium text-sm">Auto-Restart on Failure</div>
                <div class="text-xs text-gray-500">Automatically restart the app after consecutive failures</div>
              </div>
              <USwitch v-model="healthForm.auto_restart" />
            </div>
          </template>
        </div>
      </UCard>
    </div>

    <!-- Terminal Tab -->
    <div v-if="activeTab === 'terminal'">
      <div v-if="app.status !== 'running'" class="text-center py-12">
        <UIcon name="i-heroicons-command-line" class="w-12 h-12 mx-auto mb-2 text-gray-300" />
        <p class="text-gray-500">App must be running to use terminal</p>
      </div>
      <AppsAppTerminal v-else :app-id="appId" />
    </div>

    <!-- Volumes Tab -->
    <UCard v-if="activeTab === 'volumes'">
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold">Persistent Volumes</h3>
          <div class="flex gap-2">
            <UButton variant="outline" size="sm" @click="addVolume">
              <UIcon name="i-heroicons-plus" class="w-4 h-4 mr-1" />
              Add Volume
            </UButton>
            <UButton :loading="savingVolumes" @click="saveVolumes">
              Save and Restart
            </UButton>
          </div>
        </div>
      </template>

      <div class="space-y-3">
        <div v-if="volumeList.length === 0" class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-circle-stack" class="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>No volumes configured</p>
          <p class="text-sm">Click "Add Volume" to create one. Data will not persist across container restarts without volumes.</p>
        </div>

        <div
          v-for="(volume, index) in volumeList"
          :key="index"
          class="p-4 rounded-lg bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700"
        >
          <div class="flex items-start gap-3">
            <div class="flex-1 grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label class="block text-xs text-gray-500 mb-1">Name</label>
                <input
                  v-model="volume.name"
                  type="text"
                  placeholder="volume-name"
                  class="w-full px-3 py-2 text-sm bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500"
                >
              </div>
              <div>
                <label class="block text-xs text-gray-500 mb-1">Container Path</label>
                <input
                  v-model="volume.container_path"
                  type="text"
                  placeholder="/data"
                  class="w-full px-3 py-2 font-mono text-sm bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500"
                >
              </div>
            </div>
            <div class="flex items-center gap-3 pt-5">
              <label class="flex items-center gap-1.5 text-sm text-gray-600 dark:text-gray-400 cursor-pointer whitespace-nowrap">
                <input
                  v-model="volume.read_only"
                  type="checkbox"
                  class="rounded border-gray-300 dark:border-gray-600"
                >
                Read Only
              </label>
              <UButton
                icon="i-heroicons-trash"
                color="error"
                variant="ghost"
                size="sm"
                @click="removeVolume(index)"
              />
            </div>
          </div>
          <p v-if="volume.host_path" class="mt-2 text-xs text-gray-400 font-mono">
            Host: {{ volume.host_path }}
          </p>
        </div>
      </div>
    </UCard>

    <!-- Environment Tab -->
    <UCard v-if="activeTab === 'env'">
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold">Environment Variables</h3>
          <div class="flex gap-2">
            <UButton variant="outline" size="sm" @click="addEmptyEnvVar">
              <UIcon name="i-heroicons-plus" class="w-4 h-4 mr-1" />
              Add Variable
            </UButton>
            <UButton :loading="savingEnv" @click="saveEnvVars">
              Save and Restart
            </UButton>
          </div>
        </div>
      </template>

      <div class="space-y-3">
        <div v-if="envVars.length === 0" class="text-center py-8 text-gray-500">
          <UIcon name="i-heroicons-key" class="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>No environment variables configured.</p>
          <p class="text-sm">Click "Add Variable" to create one.</p>
        </div>

        <!-- Existing Variables -->
        <div
          v-for="(envVar, index) in envVars"
          :key="index"
          class="flex items-center gap-2 p-2 rounded-lg bg-gray-50 dark:bg-gray-800/50"
        >
          <input
            v-model="envVar.key"
            type="text"
            placeholder="KEY"
            class="flex-1 px-3 py-2 font-mono text-sm bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500"
            @keydown.enter="(($event.target as HTMLElement)?.nextElementSibling?.nextElementSibling as HTMLElement)?.focus()"
          >
          <span class="text-gray-400">=</span>
          <input
            v-model="envVar.value"
            type="text"
            placeholder="value"
            class="flex-2 px-3 py-2 font-mono text-sm bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500"
            @keydown.enter="saveEnvVars"
          >
          <UButton
            icon="i-heroicons-trash"
            color="error"
            variant="ghost"
            size="sm"
            @click="removeEnvVar(index)"
          />
        </div>
      </div>
    </UCard>

    <!-- Settings Tab -->
    <div v-if="activeTab === 'settings'" class="space-y-6">
      <!-- Row 1: General + Container (or just General + Aliases for static) -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <!-- General Settings -->
        <UCard>
          <template #header>
            <h3 class="font-semibold">General</h3>
          </template>

          <div class="space-y-4">
            <UFormField label="App Name" hint="Unique identifier for your app">
              <UInput v-model="settingsForm.name" placeholder="my-app" />
            </UFormField>

            <UFormField label="Domain" hint="Primary domain for your app">
              <UInput v-model="settingsForm.domain" placeholder="app.example.com" />
            </UFormField>
          </div>
        </UCard>

        <!-- Container Settings (non-static only) -->
        <UCard v-if="app?.type !== 'static'">
          <template #header>
            <h3 class="font-semibold">Container</h3>
          </template>

          <div class="space-y-4">
            <UFormField label="Image" hint="Docker image (changes require redeploy)">
              <UInput v-model="settingsForm.image" placeholder="nginx:latest" />
            </UFormField>

            <UFormField label="Container Port" hint="Port your app listens on">
              <UInput v-model.number="settingsForm.port" type="number" placeholder="8080" />
            </UFormField>

            <div class="text-sm text-gray-500">
              <strong>Container ID:</strong>
              <code class="ml-2 px-2 py-0.5 bg-gray-100 dark:bg-gray-800 rounded">{{ app?.container_id ? app.container_id.slice(0, 12) : 'Not deployed' }}</code>
            </div>
          </div>
        </UCard>

        <!-- Domain Aliases (shown here for static sites to fill the 2-col row) -->
        <UCard v-if="app?.type === 'static'">
          <template #header>
            <div class="flex items-center justify-between">
              <h3 class="font-semibold">Domain Aliases</h3>
              <UButton :loading="savingAliases" size="xs" @click="saveAliases">
                Save
              </UButton>
            </div>
          </template>

          <div class="space-y-3">
            <div class="flex gap-2">
              <UInput
                v-model="newAlias"
                placeholder="example.com"
                size="sm"
                class="flex-1"
                @keydown.enter="addAlias"
              />
              <UButton variant="outline" size="sm" @click="addAlias">
                <UIcon name="i-heroicons-plus" class="w-4 h-4" />
              </UButton>
            </div>

            <div v-if="aliases.length === 0" class="text-center py-3 text-gray-500">
              <p class="text-sm">No aliases configured</p>
            </div>

            <div v-else class="space-y-1.5 max-h-48 overflow-y-auto">
              <div
                v-for="(alias, index) in aliases"
                :key="alias"
                class="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-800/50 rounded-lg text-sm"
              >
                <div class="flex items-center gap-2 min-w-0">
                  <UIcon name="i-heroicons-globe-alt" class="w-3.5 h-3.5 text-gray-400 shrink-0" />
                  <span class="font-mono text-xs truncate">{{ alias }}</span>
                </div>
                <div class="flex items-center gap-1 shrink-0">
                  <a
                    :href="`https://${alias}`"
                    target="_blank"
                    class="text-gray-400 hover:text-primary-500"
                  >
                    <UIcon name="i-heroicons-arrow-top-right-on-square" class="w-3.5 h-3.5" />
                  </a>
                  <UButton
                    icon="i-heroicons-trash"
                    color="error"
                    variant="ghost"
                    size="xs"
                    @click="removeAlias(index)"
                  />
                </div>
              </div>
            </div>
          </div>
        </UCard>
      </div>

      <!-- Row 2: Resources + Network + Aliases (non-static only) -->
      <div v-if="app?.type !== 'static'" class="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <!-- Resource Limits -->
        <UCard>
          <template #header>
            <h3 class="font-semibold">Resource Limits</h3>
          </template>

          <div class="space-y-4">
            <UFormField label="Memory Limit (MB)" hint="0 for unlimited">
              <UInput v-model.number="settingsForm.memory" type="number" placeholder="512" />
            </UFormField>

            <UFormField label="CPU Limit (cores)" hint="0 for unlimited">
              <UInput v-model.number="settingsForm.cpus" type="number" step="0.1" placeholder="1.0" />
            </UFormField>
          </div>
        </UCard>

        <!-- Network & Access -->
        <UCard>
          <template #header>
            <h3 class="font-semibold">Network</h3>
          </template>

          <div class="space-y-4">
            <div class="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
              <div>
                <div class="font-medium text-sm">External Access</div>
                <div class="text-xs text-gray-500">Direct TCP from outside</div>
              </div>
              <USwitch v-model="settingsForm.exposeExternal" />
            </div>

            <div v-if="settingsForm.exposeExternal && app?.ports?.host_port" class="p-3 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg text-sm">
              <div class="flex items-start gap-2">
                <UIcon name="i-heroicons-exclamation-triangle" class="w-5 h-5 text-yellow-500 shrink-0" />
                <span class="text-yellow-600 dark:text-yellow-400">Port {{ app.ports.host_port }} is accessible from the internet.</span>
              </div>
            </div>

            <div class="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg text-sm">
              <div class="flex items-start gap-2">
                <UIcon name="i-heroicons-information-circle" class="w-5 h-5 text-blue-500 shrink-0 mt-0.5" />
                <span class="text-blue-600 dark:text-blue-400"><strong>SSL</strong> auto-managed by Caddy.</span>
              </div>
            </div>
          </div>
        </UCard>

        <!-- Domain Aliases -->
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <h3 class="font-semibold">Domain Aliases</h3>
              <UButton :loading="savingAliases" size="xs" @click="saveAliases">
                Save
              </UButton>
            </div>
          </template>

          <div class="space-y-3">
            <div class="flex gap-2">
              <UInput
                v-model="newAlias"
                placeholder="example.com"
                size="sm"
                class="flex-1"
                @keydown.enter="addAlias"
              />
              <UButton variant="outline" size="sm" @click="addAlias">
                <UIcon name="i-heroicons-plus" class="w-4 h-4" />
              </UButton>
            </div>

            <div v-if="aliases.length === 0" class="text-center py-3 text-gray-500">
              <p class="text-sm">No aliases configured</p>
            </div>

            <div v-else class="space-y-1.5 max-h-48 overflow-y-auto">
              <div
                v-for="(alias, index) in aliases"
                :key="alias"
                class="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-800/50 rounded-lg text-sm"
              >
                <div class="flex items-center gap-2 min-w-0">
                  <UIcon name="i-heroicons-globe-alt" class="w-3.5 h-3.5 text-gray-400 shrink-0" />
                  <span class="font-mono text-xs truncate">{{ alias }}</span>
                </div>
                <div class="flex items-center gap-1 shrink-0">
                  <a
                    :href="`https://${alias}`"
                    target="_blank"
                    class="text-gray-400 hover:text-primary-500"
                  >
                    <UIcon name="i-heroicons-arrow-top-right-on-square" class="w-3.5 h-3.5" />
                  </a>
                  <UButton
                    icon="i-heroicons-trash"
                    color="error"
                    variant="ghost"
                    size="xs"
                    @click="removeAlias(index)"
                  />
                </div>
              </div>
            </div>
          </div>
        </UCard>
      </div>

      <!-- Save Button -->
      <div class="flex justify-end">
        <UButton :loading="savingSettings" @click="saveSettings">
          Save All Changes
        </UButton>
      </div>

      <!-- Webhook Setup -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Webhook (Auto-Deploy)</h3>
            <template v-if="app?.deployment?.webhook_secret">
              <div class="flex gap-2">
                <UButton variant="outline" size="xs" @click="regenerateSecret" :loading="webhookSetupLoading">
                  Regenerate Secret
                </UButton>
                <UButton color="error" variant="outline" size="xs" @click="disableWebhook" :loading="webhookSetupLoading">
                  Disable
                </UButton>
              </div>
            </template>
          </div>
        </template>

        <!-- Webhook not configured -->
        <div v-if="!app?.deployment?.webhook_secret" class="space-y-4">
          <p class="text-sm text-gray-500">
            Enable webhooks to automatically deploy when you push to GitHub.
          </p>
          <UFormField label="Git Repository URL" hint="HTTPS clone URL">
            <UInput v-model="webhookGitUrl" placeholder="https://github.com/user/repo.git" />
          </UFormField>
          <UButton :loading="webhookSetupLoading" @click="setupWebhook">
            Enable Webhook
          </UButton>
        </div>

        <!-- Webhook configured -->
        <div v-else class="space-y-4">
          <div class="p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
            <div class="flex items-start gap-2">
              <UIcon name="i-heroicons-check-circle" class="w-5 h-5 text-green-500 shrink-0 mt-0.5" />
              <span class="text-sm text-green-700 dark:text-green-300">Webhook is active. Push to <code class="font-mono bg-green-100 dark:bg-green-800 px-1 rounded">{{ app.deployment?.branch || 'main' }}</code> to auto-deploy.</span>
            </div>
          </div>

          <div class="space-y-3">
            <div>
              <label class="block text-xs text-gray-500 mb-1">Webhook URL</label>
              <div class="flex gap-2">
                <code class="flex-1 px-3 py-2 text-sm bg-gray-100 dark:bg-gray-800 rounded-md font-mono overflow-x-auto break-all">{{ app.domain ? `https://${app.domain}/api/apps/${app.id}/webhook` : `/api/apps/${app.id}/webhook` }}</code>
                <UButton variant="outline" size="sm" icon="i-heroicons-clipboard" @click="copyToClipboard(app.domain ? `https://${app.domain}/api/apps/${app.id}/webhook` : `/api/apps/${app.id}/webhook`)" />
              </div>
            </div>

            <div>
              <label class="block text-xs text-gray-500 mb-1">Secret</label>
              <div class="flex gap-2">
                <code class="flex-1 px-3 py-2 text-sm bg-gray-100 dark:bg-gray-800 rounded-md font-mono">{{ showWebhookSecret ? app.deployment?.webhook_secret : '••••••••••••••••' }}</code>
                <UButton variant="outline" size="sm" :icon="showWebhookSecret ? 'i-heroicons-eye-slash' : 'i-heroicons-eye'" @click="showWebhookSecret = !showWebhookSecret" />
                <UButton variant="outline" size="sm" icon="i-heroicons-clipboard" @click="copyToClipboard(app.deployment?.webhook_secret || '')" />
              </div>
            </div>

            <div>
              <label class="block text-xs text-gray-500 mb-1">Git URL</label>
              <code class="block px-3 py-2 text-sm bg-gray-100 dark:bg-gray-800 rounded-md font-mono break-all">{{ app.deployment?.git_url }}</code>
            </div>
          </div>

          <div class="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg text-sm">
            <div class="flex items-start gap-2">
              <UIcon name="i-heroicons-information-circle" class="w-5 h-5 text-blue-500 shrink-0 mt-0.5" />
              <div class="text-blue-700 dark:text-blue-300">
                <strong>GitHub Setup:</strong> Go to your repository Settings &rarr; Webhooks &rarr; Add webhook. Paste the URL and secret above. Set content type to <code class="font-mono bg-blue-100 dark:bg-blue-800 px-1 rounded">application/json</code>.
              </div>
            </div>
          </div>
        </div>
      </UCard>

      <!-- Danger Zone -->
      <UCard class="border-red-200 dark:border-red-800">
        <template #header>
          <h3 class="font-semibold text-red-600 dark:text-red-400">Danger Zone</h3>
        </template>

        <div class="flex items-center justify-between p-4 border border-red-200 dark:border-red-800 rounded-lg">
          <div>
            <h4 class="font-medium">Delete this app</h4>
            <p class="text-sm text-gray-500">Once deleted, this app and all its data will be permanently removed.</p>
          </div>
          <UButton color="error" variant="outline" @click="showDeleteModal = true">
            Delete App
          </UButton>
        </div>
      </UCard>
    </div>
    </div>

    <div v-else class="text-center py-12">
      <UIcon name="i-heroicons-exclamation-circle" class="w-16 h-16 mx-auto mb-4 text-gray-300" />
      <h3 class="text-lg font-medium">App not found</h3>
      <UButton to="/apps" variant="soft" class="mt-4">Back to Apps</UButton>
    </div>

    <!-- Delete Confirmation Modal -->
    <ConfirmationModal
      v-model:open="showDeleteModal"
      title="Delete App"
      :message="`Are you sure you want to delete ${app?.name}? This cannot be undone.`"
      confirm-text="Delete"
      confirm-color="error"
      icon="i-heroicons-trash"
      @confirm="deleteApp"
    />

    <!-- Restart Reminder Modal -->
    <UModal v-model:open="showRestartModal">
      <template #content>
        <div class="p-6">
          <div class="flex items-center gap-3 mb-4">
            <div class="flex items-center justify-center w-10 h-10 rounded-full bg-yellow-100 dark:bg-yellow-900/30">
              <UIcon name="i-heroicons-arrow-path" class="w-5 h-5 text-yellow-600 dark:text-yellow-400" />
            </div>
            <h3 class="text-lg font-semibold">Restart Required</h3>
          </div>
          <p class="text-gray-600 dark:text-gray-400 mb-6">
            External access setting has been saved. You need to <strong>restart the app</strong> for the change to take effect.
          </p>
          <div class="flex justify-end gap-3">
            <UButton variant="outline" @click="showRestartModal = false">
              Later
            </UButton>
            <UButton color="primary" @click="showRestartModal = false; restartApp()">
              Restart Now
            </UButton>
          </div>
        </div>
      </template>
    </UModal>
  </div>
</template>
