<script setup lang="ts">
import type { HealthResponse } from '~/types'
import type { ConfigResponse } from '~/composables/useApi'

interface VersionInfo {
  current: string
  latest: string
  updateAvailable: boolean
}

definePageMeta({
  title: 'Settings'
})

const toast = useToast()
const { data: health, refresh: refreshHealth } = await useApiFetch<HealthResponse>('/health')
const { data: configData } = await useApiFetch<ConfigResponse>('/system/config')

// Version info
const version = ref<VersionInfo | null>(null)
const checkingVersion = ref(false)
const updating = ref(false)
const updateMessage = ref('')
const updateError = ref('')

const checkVersion = async () => {
  checkingVersion.value = true
  updateError.value = ''
  try {
    version.value = await $api<VersionInfo>('/system/version')
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    updateError.value = err.data?.error || 'Failed to check version'
  } finally {
    checkingVersion.value = false
  }
}

const waitForServer = async (maxAttempts = 10, delayMs = 2000): Promise<boolean> => {
  for (let i = 0; i < maxAttempts; i++) {
    await new Promise(resolve => setTimeout(resolve, delayMs))
    try {
      await $api('/health')
      return true
    } catch {
      // Server not ready yet
    }
  }
  return false
}

const performUpdate = async () => {
  updating.value = true
  updateMessage.value = ''
  updateError.value = ''
  try {
    const result = await $api<{ status: string; message: string }>('/system/update', {
      method: 'POST'
    })
    updateMessage.value = result.message

    // If server is restarting, wait for it to come back
    if (result.message?.includes('Restarting')) {
      updateMessage.value = 'Update complete. Restarting service...'
      const serverReady = await waitForServer()
      if (serverReady) {
        updateMessage.value = 'Update successful! Refreshing...'
        await new Promise(resolve => setTimeout(resolve, 1000))
        window.location.reload()
      } else {
        updateError.value = 'Server did not restart in time. Please refresh manually.'
      }
    } else {
      // Refresh version after update
      await checkVersion()
    }
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    // Connection error during restart is expected
    if (err.data?.error) {
      updateError.value = err.data.error
    } else {
      // Assume restart in progress, wait for server
      updateMessage.value = 'Update complete. Restarting service...'
      const serverReady = await waitForServer()
      if (serverReady) {
        updateMessage.value = 'Update successful! Refreshing...'
        await new Promise(resolve => setTimeout(resolve, 1000))
        window.location.reload()
      } else {
        updateError.value = 'Server did not restart in time. Please refresh manually.'
      }
    }
  } finally {
    updating.value = false
  }
}

// Check version on load
onMounted(() => {
  checkVersion()
  // Load domain settings from config
  if (configData.value?.domain) {
    settings.value.domain = configData.value.domain.base || ''
    settings.value.enableWildcard = configData.value.domain.wildcard ?? true
  }
})

const settings = ref({
  domain: '',
  email: '',
  enableWildcard: true
})

// Domain settings
const savingDomain = ref(false)
const domainSuccess = ref(false)
const domainError = ref('')

const saveDomainSettings = async () => {
  savingDomain.value = true
  domainError.value = ''
  domainSuccess.value = false
  try {
    await $api('/system/config', {
      method: 'PUT',
      body: {
        domain: {
          base: settings.value.domain,
          wildcard: settings.value.enableWildcard
        }
      }
    })
    domainSuccess.value = true
    toast.add({ title: 'Domain settings saved', color: 'success' })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    domainError.value = err.data?.error || 'Failed to save domain settings'
  } finally {
    savingDomain.value = false
  }
}

// Service restart
const restartingService = ref<string | null>(null)

const restartService = async (service: string) => {
  restartingService.value = service
  try {
    await $api(`/system/restart/${service}`, { method: 'POST' })
    toast.add({ title: `${service} restarted`, color: 'success' })
    // If restarting deployer, wait and refresh
    if (service === 'basepod') {
      toast.add({ title: 'Waiting for server...', color: 'info' })
      const ready = await waitForServer()
      if (ready) {
        window.location.reload()
      }
    } else {
      // Refresh health status
      await refreshHealth()
    }
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: `Failed to restart ${service}`, description: err.data?.error, color: 'error' })
  } finally {
    restartingService.value = null
  }
}

const passwordForm = ref({
  currentPassword: '',
  newPassword: '',
  confirmPassword: ''
})
const passwordError = ref('')
const passwordSuccess = ref(false)
const changingPassword = ref(false)

const changePassword = async () => {
  passwordError.value = ''
  passwordSuccess.value = false

  if (passwordForm.value.newPassword !== passwordForm.value.confirmPassword) {
    passwordError.value = 'Passwords do not match'
    return
  }

  if (passwordForm.value.newPassword.length < 8) {
    passwordError.value = 'Password must be at least 8 characters'
    return
  }

  changingPassword.value = true
  try {
    await $api('/auth/change-password', {
      method: 'POST',
      body: {
        currentPassword: passwordForm.value.currentPassword,
        newPassword: passwordForm.value.newPassword
      }
    })
    passwordSuccess.value = true
    passwordForm.value = { currentPassword: '', newPassword: '', confirmPassword: '' }
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    passwordError.value = err.data?.error || 'Failed to change password'
  } finally {
    changingPassword.value = false
  }
}

// Prune resources
const pruning = ref(false)
const pruneResult = ref('')
const pruneError = ref('')

const pruneResources = async () => {
  if (!confirm('This will remove all unused containers, images, and volumes. Continue?')) {
    return
  }

  pruning.value = true
  pruneResult.value = ''
  pruneError.value = ''
  try {
    const result = await $api<{ status: string; output: string }>('/system/prune', {
      method: 'POST'
    })
    pruneResult.value = result.output || 'Prune completed successfully'
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    pruneError.value = err.data?.error || 'Prune failed'
  } finally {
    pruning.value = false
  }
}
</script>

<template>
  <div>
    <div class="max-w-3xl space-y-6">
      <!-- System Update -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">System Update</h3>
            <UBadge v-if="version?.updateAvailable" color="warning">Update Available</UBadge>
          </div>
        </template>

        <div class="space-y-4">
          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium">Current Version</p>
              <p class="text-2xl font-mono">v{{ version?.current || '...' }}</p>
            </div>
            <div class="text-right">
              <p class="text-sm text-gray-500">Latest Version</p>
              <p class="text-xl font-mono">v{{ version?.latest || '...' }}</p>
            </div>
          </div>

          <UAlert
            v-if="version?.updateAvailable"
            color="info"
            variant="soft"
            title="A new version is available"
            description="Click 'Update Now' to download and install the latest version."
          />

          <UAlert
            v-if="updateMessage"
            color="success"
            variant="soft"
            :title="updateMessage"
          />

          <UAlert
            v-if="updateError"
            color="error"
            variant="soft"
            :title="updateError"
          />

          <div class="flex gap-2">
            <UButton
              variant="soft"
              :loading="checkingVersion"
              @click="checkVersion"
            >
              Check for Updates
            </UButton>
            <UButton
              v-if="version?.updateAvailable"
              color="primary"
              :loading="updating"
              @click="performUpdate"
            >
              Update Now
            </UButton>
          </div>
        </div>
      </UCard>

      <!-- Domain Settings -->
      <UCard>
        <template #header>
          <h3 class="font-semibold">Domain Settings</h3>
        </template>

        <form class="space-y-4" @submit.prevent="saveDomainSettings">
          <UFormField label="Root Domain" help="The base domain for your apps (e.g., example.com)">
            <UInput v-model="settings.domain" placeholder="example.com" />
          </UFormField>

          <UFormField>
            <UCheckbox v-model="settings.enableWildcard" label="Enable wildcard subdomains" />
          </UFormField>

          <UAlert
            v-if="domainError"
            color="error"
            variant="soft"
            :title="domainError"
          />

          <UAlert
            v-if="domainSuccess"
            color="success"
            variant="soft"
            title="Domain settings saved successfully"
          />

          <UButton type="submit" :loading="savingDomain">
            Save Domain Settings
          </UButton>
        </form>
      </UCard>

      <!-- Change Password -->
      <UCard>
        <template #header>
          <h3 class="font-semibold">Change Password</h3>
        </template>

        <form class="space-y-4" @submit.prevent="changePassword">
          <UFormField label="Current Password">
            <UInput
              v-model="passwordForm.currentPassword"
              type="password"
              placeholder="Enter current password"
            />
          </UFormField>

          <UFormField label="New Password">
            <UInput
              v-model="passwordForm.newPassword"
              type="password"
              placeholder="Enter new password"
            />
          </UFormField>

          <UFormField label="Confirm New Password">
            <UInput
              v-model="passwordForm.confirmPassword"
              type="password"
              placeholder="Confirm new password"
            />
          </UFormField>

          <UAlert
            v-if="passwordError"
            color="error"
            variant="soft"
            :title="passwordError"
          />

          <UAlert
            v-if="passwordSuccess"
            color="success"
            variant="soft"
            title="Password changed successfully"
          />

          <UButton type="submit" :loading="changingPassword">
            Change Password
          </UButton>
        </form>
      </UCard>

      <!-- System Status -->
      <UCard>
        <template #header>
          <h3 class="font-semibold">System Status</h3>
        </template>

        <div class="space-y-4">
          <div class="flex items-center justify-between py-2 border-b border-gray-200 dark:border-gray-800">
            <div>
              <p class="font-medium">Podman</p>
              <p class="text-sm text-gray-500">Container runtime</p>
            </div>
            <div class="flex items-center gap-2">
              <UBadge :color="health?.podman === 'connected' ? 'success' : 'error'">
                {{ health?.podman === 'connected' ? 'Connected' : 'Disconnected' }}
              </UBadge>
              <UButton
                size="xs"
                variant="soft"
                :loading="restartingService === 'podman'"
                @click="restartService('podman')"
              >
                Restart
              </UButton>
            </div>
          </div>
          <p v-if="health?.podman !== 'connected'" class="text-xs text-red-500 -mt-2">
            {{ health?.podman_error }}
          </p>

          <div class="flex items-center justify-between py-2 border-b border-gray-200 dark:border-gray-800">
            <div>
              <p class="font-medium">Caddy</p>
              <p class="text-sm text-gray-500">Reverse proxy</p>
            </div>
            <div class="flex items-center gap-2">
              <UBadge color="success">Running</UBadge>
              <UButton
                size="xs"
                variant="soft"
                :loading="restartingService === 'caddy'"
                @click="restartService('caddy')"
              >
                Restart
              </UButton>
            </div>
          </div>

          <div class="flex items-center justify-between py-2 border-b border-gray-200 dark:border-gray-800">
            <div>
              <p class="font-medium">Deployer</p>
              <p class="text-sm text-gray-500">API Server</p>
            </div>
            <div class="flex items-center gap-2">
              <UBadge color="success">Running</UBadge>
              <UButton
                size="xs"
                variant="soft"
                :loading="restartingService === 'basepod'"
                @click="restartService('basepod')"
              >
                Restart
              </UButton>
            </div>
          </div>

          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium">Database</p>
              <p class="text-sm text-gray-500">SQLite</p>
            </div>
            <UBadge color="success">Connected</UBadge>
          </div>
        </div>
      </UCard>

      <!-- Danger Zone -->
      <UCard class="border-red-200 dark:border-red-900">
        <template #header>
          <h3 class="font-semibold text-red-600">Danger Zone</h3>
        </template>

        <div class="space-y-4">
          <UAlert
            v-if="pruneResult"
            color="success"
            variant="soft"
            title="Prune completed"
          >
            <pre class="text-xs mt-2 whitespace-pre-wrap">{{ pruneResult }}</pre>
          </UAlert>

          <UAlert
            v-if="pruneError"
            color="error"
            variant="soft"
            :title="pruneError"
          />

          <div class="flex items-center justify-between">
            <div>
              <p class="font-medium">Prune Unused Resources</p>
              <p class="text-sm text-gray-500">Remove unused containers, images, and volumes</p>
            </div>
            <UButton color="error" variant="soft" :loading="pruning" @click="pruneResources">
              Prune
            </UButton>
          </div>
        </div>
      </UCard>
    </div>
  </div>
</template>
