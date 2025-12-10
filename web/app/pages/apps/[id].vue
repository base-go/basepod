<script setup lang="ts">
import type { App } from '~/types'

const route = useRoute()
const toast = useToast()
const appId = route.params.id as string

const { data: app, refresh } = await useApiFetch<App>(`/apps/${appId}`)

const activeTab = ref('overview')

const tabs = [
  { label: 'Overview', value: 'overview', icon: 'i-heroicons-information-circle' },
  { label: 'Logs', value: 'logs', icon: 'i-heroicons-document-text' },
  { label: 'Environment', value: 'env', icon: 'i-heroicons-key' },
  { label: 'Settings', value: 'settings', icon: 'i-heroicons-cog-6-tooth' }
]

const logs = ref('')
const logsLoading = ref(false)

async function fetchLogs() {
  if (!app.value?.container_id) return
  logsLoading.value = true
  try {
    logs.value = await $api<string>(`/apps/${appId}/logs?tail=100`)
  } catch {
    logs.value = ''
  } finally {
    logsLoading.value = false
  }
}

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
  domain: '',
  port: 8080,
  enableSSL: false
})

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

// Initialize settings form when app data loads
watch(() => app.value, (appData) => {
  if (appData) {
    settingsForm.value = {
      domain: appData.domain || '',
      port: appData.ports?.container_port || 8080,
      enableSSL: appData.ssl?.enabled || false
    }
  }
}, { immediate: true })

async function saveSettings() {
  try {
    await $api(`/apps/${appId}`, {
      method: 'PUT',
      body: {
        domain: settingsForm.value.domain,
        port: settingsForm.value.port,
        enable_ssl: settingsForm.value.enableSSL
      }
    })
    toast.add({ title: 'Settings saved', color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to save', description: getErrorMessage(error), color: 'error' })
  }
}

async function deleteApp() {
  if (!confirm(`Are you sure you want to delete ${app.value?.name}? This cannot be undone.`)) return

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
          <p v-if="app.domain" class="text-gray-500">
            <a :href="`https://${app.domain}`" target="_blank" class="hover:text-primary-500">
              {{ app.domain }}
            </a>
          </p>
        </div>
      </div>

      <div class="flex items-center gap-3">
        <UBadge
          :color="app.status === 'running' ? 'success' : app.status === 'stopped' ? 'warning' : 'neutral'"
          size="lg"
        >
          {{ app.status }}
        </UBadge>

        <div class="flex gap-2">
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
        </div>
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
          <div class="flex justify-between">
            <dt class="text-gray-500">Created</dt>
            <dd>{{ new Date(app.created_at).toLocaleString() }}</dd>
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
            <dd>{{ app.resources?.cpus || 'Unlimited' }}</dd>
          </div>
          <div class="flex justify-between">
            <dt class="text-gray-500">SSL</dt>
            <dd>
              <UBadge :color="app.ssl?.enabled ? 'success' : 'neutral'">
                {{ app.ssl?.enabled ? 'Enabled' : 'Disabled' }}
              </UBadge>
            </dd>
          </div>
        </dl>
      </UCard>
    </div>

    <!-- Logs Tab -->
    <UCard v-if="activeTab === 'logs'">
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold">Container Logs</h3>
          <UButton variant="ghost" size="sm" icon="i-heroicons-arrow-path" :loading="logsLoading" @click="fetchLogs">
            Refresh
          </UButton>
        </div>
      </template>

      <div v-if="!app.container_id" class="text-center py-8 text-gray-500">
        <UIcon name="i-heroicons-document-text" class="w-12 h-12 mx-auto mb-2 opacity-50" />
        <p>App has not been deployed yet</p>
      </div>
      <pre v-else class="bg-gray-900 text-gray-100 p-4 rounded-lg overflow-x-auto text-sm font-mono max-h-[500px] overflow-y-auto">{{ logs || 'No logs available' }}</pre>
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
              Save Changes
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
            class="flex-[2] px-3 py-2 font-mono text-sm bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-md focus:outline-none focus:ring-2 focus:ring-primary-500"
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
    <UCard v-if="activeTab === 'settings'">
      <template #header>
        <h3 class="font-semibold">App Settings</h3>
      </template>

      <div class="space-y-4 max-w-md">
        <UFormField label="Domain">
          <UInput v-model="settingsForm.domain" placeholder="app.example.com" />
        </UFormField>

        <UFormField label="Container Port">
          <UInput v-model.number="settingsForm.port" type="number" />
        </UFormField>

        <UFormField>
          <UCheckbox v-model="settingsForm.enableSSL" label="Enable SSL" />
        </UFormField>

        <div class="flex gap-2 pt-4">
          <UButton @click="saveSettings">Save Changes</UButton>
          <UButton color="error" variant="outline" @click="deleteApp">Delete App</UButton>
        </div>
      </div>
    </UCard>
  </div>

  <div v-else class="text-center py-12">
    <UIcon name="i-heroicons-exclamation-circle" class="w-16 h-16 mx-auto mb-4 text-gray-300" />
    <h3 class="text-lg font-medium">App not found</h3>
    <UButton to="/apps" variant="soft" class="mt-4">Back to Apps</UButton>
  </div>
</template>
