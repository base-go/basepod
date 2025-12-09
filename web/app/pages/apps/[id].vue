<script setup lang="ts">
const route = useRoute()
const appId = route.params.id as string

const { data: app, refresh } = await useFetch(`/api/apps/${appId}`)
const { data: logs } = await useFetch(`/api/apps/${appId}/logs?tail=100`, {
  transform: (data: string) => data
})

const activeTab = ref('overview')

const tabs = [
  { key: 'overview', label: 'Overview', icon: 'i-heroicons-information-circle' },
  { key: 'logs', label: 'Logs', icon: 'i-heroicons-document-text' },
  { key: 'env', label: 'Environment', icon: 'i-heroicons-key' },
  { key: 'settings', label: 'Settings', icon: 'i-heroicons-cog-6-tooth' }
]

async function startApp() {
  await $fetch(`/api/apps/${appId}/start`, { method: 'POST' })
  refresh()
}

async function stopApp() {
  await $fetch(`/api/apps/${appId}/stop`, { method: 'POST' })
  refresh()
}

async function restartApp() {
  await $fetch(`/api/apps/${appId}/restart`, { method: 'POST' })
  refresh()
}
</script>

<template>
  <div v-if="app">
    <template #header>
      {{ app.name }}
    </template>

    <!-- App Header -->
    <div class="flex items-start justify-between mb-6">
      <div class="flex items-center gap-4">
        <div class="flex items-center justify-center w-16 h-16 rounded-xl bg-primary-100 dark:bg-primary-900/20">
          <UIcon name="i-heroicons-cube" class="w-8 h-8 text-primary-500" />
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

      <div class="flex items-center gap-2">
        <UBadge
          :color="app.status === 'running' ? 'success' : app.status === 'stopped' ? 'warning' : 'gray'"
          size="lg"
        >
          {{ app.status }}
        </UBadge>

        <UButtonGroup>
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
            variant="soft"
            @click="restartApp"
          >
            Restart
          </UButton>
        </UButtonGroup>
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
            <dd class="font-mono text-sm">{{ app.id }}</dd>
          </div>
          <div class="flex justify-between">
            <dt class="text-gray-500">Container ID</dt>
            <dd class="font-mono text-sm">{{ app.container_id || '-' }}</dd>
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
              <UBadge :color="app.ssl?.enabled ? 'success' : 'gray'">
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
          <UButton variant="ghost" size="sm" icon="i-heroicons-arrow-path" @click="refresh">
            Refresh
          </UButton>
        </div>
      </template>

      <pre class="bg-gray-900 text-gray-100 p-4 rounded-lg overflow-x-auto text-sm font-mono max-h-96 overflow-y-auto">{{ logs || 'No logs available' }}</pre>
    </UCard>

    <!-- Environment Tab -->
    <UCard v-if="activeTab === 'env'">
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold">Environment Variables</h3>
          <UButton variant="soft" size="sm" icon="i-heroicons-plus">
            Add Variable
          </UButton>
        </div>
      </template>

      <div v-if="app.env && Object.keys(app.env).length" class="space-y-2">
        <div
          v-for="(value, key) in app.env"
          :key="key"
          class="flex items-center justify-between p-3 rounded-lg bg-gray-50 dark:bg-gray-800"
        >
          <code class="font-mono">{{ key }}</code>
          <code class="font-mono text-gray-500">{{ value }}</code>
        </div>
      </div>

      <div v-else class="text-center py-8 text-gray-500">
        No environment variables configured
      </div>
    </UCard>

    <!-- Settings Tab -->
    <UCard v-if="activeTab === 'settings'">
      <template #header>
        <h3 class="font-semibold">App Settings</h3>
      </template>

      <div class="space-y-4 max-w-md">
        <UFormField label="Domain">
          <UInput :model-value="app.domain" placeholder="app.example.com" />
        </UFormField>

        <UFormField label="Container Port">
          <UInput :model-value="app.ports?.container_port" type="number" />
        </UFormField>

        <UFormField>
          <UCheckbox :model-value="app.ssl?.enabled" label="Enable SSL" />
        </UFormField>

        <UButton>Save Changes</UButton>
      </div>
    </UCard>
  </div>

  <div v-else class="text-center py-12">
    <UIcon name="i-heroicons-exclamation-circle" class="w-16 h-16 mx-auto mb-4 text-gray-300" />
    <h3 class="text-lg font-medium">App not found</h3>
    <UButton to="/apps" variant="soft" class="mt-4">Back to Apps</UButton>
  </div>
</template>
