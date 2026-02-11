<script setup lang="ts">
import type { HealthResponse } from '~/types'
import type { ConfigResponse } from '~/composables/useApi'

interface VersionInfo {
  current: string
  latest: string
  updateAvailable: boolean
}

interface BackupItem {
  id: string
  created_at: string
  size: number
  size_human: string
  path: string
  contents: {
    database: boolean
    config: boolean
    static_sites: string[]
    volumes: string[]
  }
}

interface RestoreResult {
  success: boolean
  database: boolean
  config_files: string[]
  static_sites: string[]
  volumes: string[]
  warnings: string[]
  message: string
}

definePageMeta({
  title: 'Settings'
})

const toast = useToast()
const { data: health, refresh: refreshHealth } = await useApiFetch<HealthResponse>('/health')
const { data: configData } = await useApiFetch<ConfigResponse>('/system/config')

// Tabs
const route = useRoute()
const tabs = [
  { label: 'General', value: 'general', slot: 'general', icon: 'i-heroicons-cog-6-tooth' },
  { label: 'Users', value: 'users', slot: 'users', icon: 'i-heroicons-users' },
  { label: 'Security', value: 'security', slot: 'security', icon: 'i-heroicons-shield-check' },
  { label: 'Notifications', value: 'notifications', slot: 'notifications', icon: 'i-heroicons-bell' },
  { label: 'Backup', value: 'backup', slot: 'backup', icon: 'i-heroicons-cloud-arrow-up' },
  { label: 'System', value: 'system', slot: 'system', icon: 'i-heroicons-server' }
]
const defaultTab = computed(() => {
  const tab = route.query.tab as string
  if (tab && tabs.some(t => t.value === tab)) return tab
  return 'general'
})

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

const waitForServer = async (maxAttempts = 30, delayMs = 1000): Promise<boolean> => {
  updateMessage.value = 'Waiting for server to restart...'
  for (let i = 0; i < 5; i++) {
    await new Promise(resolve => setTimeout(resolve, 1000))
    try {
      await fetch('/api/health', { method: 'GET' })
    } catch {
      break
    }
  }

  updateMessage.value = 'Server restarting...'
  for (let i = 0; i < maxAttempts; i++) {
    await new Promise(resolve => setTimeout(resolve, delayMs))
    try {
      const response = await fetch('/api/health', { method: 'GET' })
      if (response.ok) {
        return true
      }
    } catch {
      // Server not ready yet
    }
    updateMessage.value = 'Waiting for server...'
  }
  return false
}

const performUpdate = async () => {
  updating.value = true
  updateMessage.value = 'Starting update...'
  updateError.value = ''
  try {
    const result = await $api<{ status: string; message: string }>('/system/update', {
      method: 'POST'
    })
    updateMessage.value = result.message || 'Update initiated...'

    const serverReady = await waitForServer()
    if (serverReady) {
      updateMessage.value = 'Update successful! Refreshing...'
      await new Promise(resolve => setTimeout(resolve, 500))
      window.location.reload()
    } else {
      updateError.value = 'Server did not restart in time. Please refresh manually.'
      updating.value = false
    }
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    if (err.data?.error && !err.data.error.includes('fetch')) {
      updateError.value = err.data.error
      updating.value = false
    } else {
      updateMessage.value = 'Update in progress...'
      const serverReady = await waitForServer()
      if (serverReady) {
        updateMessage.value = 'Update successful! Refreshing...'
        await new Promise(resolve => setTimeout(resolve, 500))
        window.location.reload()
      } else {
        updateError.value = 'Server did not restart in time. Please refresh manually.'
        updating.value = false
      }
    }
  }
}

// Initialize settings from configData
const settings = ref({
  domain: configData.value?.domain?.root || '',
  email: '',
  enableWildcard: configData.value?.domain?.wildcard ?? true,
})

// Check version on load
onMounted(() => {
  checkVersion()
  loadBackups()
  loadUsers()
  loadNotifications()
  loadDeployTokens()
})

// Watch for configData changes
watch(configData, (newConfig) => {
  if (newConfig?.domain) {
    settings.value.domain = newConfig.domain.root || ''
    settings.value.enableWildcard = newConfig.domain.wildcard ?? true
  }
}, { immediate: true })

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
          root: settings.value.domain,
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
    if (service === 'basepod') {
      toast.add({ title: 'Waiting for server...', color: 'info' })
      const ready = await waitForServer()
      if (ready) {
        window.location.reload()
      }
    } else {
      await refreshHealth()
    }
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: `Failed to restart ${service}`, description: err.data?.error, color: 'error' })
  } finally {
    restartingService.value = null
  }
}

// Password change
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
    toast.add({ title: 'Password changed successfully', color: 'success' })
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
    toast.add({ title: 'Resources cleaned up', color: 'success' })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    pruneError.value = err.data?.error || 'Prune failed'
  } finally {
    pruning.value = false
  }
}

// ============================================================================
// Users / Team management
// ============================================================================

interface User {
  id: string
  email: string
  role: 'admin' | 'deployer' | 'viewer'
  created_at: string
  last_login_at?: string
}

const users = ref<User[]>([])
const loadingUsers = ref(false)
const showInviteForm = ref(false)
const inviteEmail = ref('')
const inviteRole = ref<'admin' | 'deployer' | 'viewer'>('viewer')
const inviting = ref(false)
const inviteResult = ref<{ invite_url: string; invite_token: string } | null>(null)
const updatingRole = ref<string | null>(null)
const deletingUser = ref<string | null>(null)

const roleOptions = [
  { label: 'Admin', value: 'admin', description: 'Full access to all features' },
  { label: 'Deployer', value: 'deployer', description: 'Deploy and manage apps' },
  { label: 'Viewer', value: 'viewer', description: 'Read-only access' }
]

async function loadUsers() {
  loadingUsers.value = true
  try {
    const data = await $api<{ users: User[] }>('/users')
    users.value = data.users || []
  } catch { users.value = [] }
  finally { loadingUsers.value = false }
}

async function inviteUser() {
  if (!inviteEmail.value) return
  inviting.value = true
  inviteResult.value = null
  try {
    const data = await $api<{ user: User; invite_url: string; invite_token: string }>('/users/invite', {
      method: 'POST',
      body: { email: inviteEmail.value, role: inviteRole.value }
    })
    inviteResult.value = { invite_url: data.invite_url, invite_token: data.invite_token }
    toast.add({ title: `Invited ${inviteEmail.value}`, color: 'success' })
    inviteEmail.value = ''
    await loadUsers()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to invite user', description: err.data?.error, color: 'error' })
  } finally { inviting.value = false }
}

async function updateUserRole(userId: string, role: string) {
  updatingRole.value = userId
  try {
    await $api(`/users/${userId}/role`, { method: 'PUT', body: { role } })
    toast.add({ title: 'Role updated', color: 'success' })
    await loadUsers()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to update role', description: err.data?.error, color: 'error' })
  } finally { updatingRole.value = null }
}

async function deleteUser(user: User) {
  if (!confirm(`Remove user ${user.email}? This cannot be undone.`)) return
  deletingUser.value = user.id
  try {
    await $api(`/users/${user.id}`, { method: 'DELETE' })
    toast.add({ title: `${user.email} removed`, color: 'success' })
    await loadUsers()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to remove user', description: err.data?.error, color: 'error' })
  } finally { deletingUser.value = null }
}

// ============================================================================
// Notifications
// ============================================================================

interface NotificationConfig {
  id: string
  name: string
  type: 'webhook' | 'slack' | 'discord'
  enabled: boolean
  scope: 'global' | 'app'
  scope_id?: string
  webhook_url?: string
  slack_webhook_url?: string
  discord_webhook_url?: string
  events: string[]
  created_at: string
  updated_at: string
}

const notifications = ref<NotificationConfig[]>([])
const loadingNotifications = ref(false)
const showNotificationForm = ref(false)
const testingNotification = ref<string | null>(null)
const deletingNotification = ref<string | null>(null)
const savingNotification = ref(false)

const notificationForm = ref({
  name: '',
  type: 'webhook' as 'webhook' | 'slack' | 'discord',
  webhook_url: '',
  slack_webhook_url: '',
  discord_webhook_url: '',
  events: [] as string[]
})

const availableEvents = [
  { value: 'deploy_success', label: 'Deploy Success' },
  { value: 'deploy_failed', label: 'Deploy Failed' },
  { value: 'health_check_fail', label: 'Health Check Failure' },
  { value: 'app_start', label: 'App Started' },
  { value: 'app_stop', label: 'App Stopped' },
  { value: 'backup_created', label: 'Backup Created' }
]

async function loadNotifications() {
  loadingNotifications.value = true
  try {
    const data = await $api<{ notifications: NotificationConfig[] }>('/notifications')
    notifications.value = data.notifications || []
  } catch { notifications.value = [] }
  finally { loadingNotifications.value = false }
}

async function createNotification() {
  savingNotification.value = true
  try {
    const body: Record<string, unknown> = {
      name: notificationForm.value.name,
      type: notificationForm.value.type,
      events: notificationForm.value.events
    }
    if (notificationForm.value.type === 'webhook') body.webhook_url = notificationForm.value.webhook_url
    if (notificationForm.value.type === 'slack') body.slack_webhook_url = notificationForm.value.slack_webhook_url
    if (notificationForm.value.type === 'discord') body.discord_webhook_url = notificationForm.value.discord_webhook_url

    await $api('/notifications', { method: 'POST', body })
    toast.add({ title: 'Notification hook created', color: 'success' })
    showNotificationForm.value = false
    notificationForm.value = { name: '', type: 'webhook', webhook_url: '', slack_webhook_url: '', discord_webhook_url: '', events: [] }
    await loadNotifications()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to create notification', description: err.data?.error, color: 'error' })
  } finally { savingNotification.value = false }
}

async function toggleNotification(notif: NotificationConfig) {
  try {
    await $api(`/notifications/${notif.id}`, { method: 'PUT', body: { enabled: !notif.enabled } })
    await loadNotifications()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to update', description: err.data?.error, color: 'error' })
  }
}

async function testNotification(id: string) {
  testingNotification.value = id
  try {
    await $api(`/notifications/${id}/test`, { method: 'POST' })
    toast.add({ title: 'Test notification sent', color: 'success' })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Test failed', description: err.data?.error, color: 'error' })
  } finally { testingNotification.value = null }
}

async function deleteNotification(notif: NotificationConfig) {
  if (!confirm(`Delete notification hook "${notif.name}"?`)) return
  deletingNotification.value = notif.id
  try {
    await $api(`/notifications/${notif.id}`, { method: 'DELETE' })
    toast.add({ title: 'Notification deleted', color: 'success' })
    await loadNotifications()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to delete', description: err.data?.error, color: 'error' })
  } finally { deletingNotification.value = null }
}

// ============================================================================
// Deploy Tokens
// ============================================================================

interface DeployToken {
  id: string
  name: string
  prefix: string
  scopes: string[]
  last_used_at?: string
  created_at: string
  expires_at?: string
}

const deployTokens = ref<DeployToken[]>([])
const loadingTokens = ref(false)
const showTokenForm = ref(false)
const creatingToken = ref(false)
const deletingToken = ref<string | null>(null)
const newTokenValue = ref('')

const tokenForm = ref({
  name: '',
  scopes: ['deploy:*']
})

async function loadDeployTokens() {
  loadingTokens.value = true
  try {
    const data = await $api<{ tokens: DeployToken[] }>('/deploy-tokens')
    deployTokens.value = data.tokens || []
  } catch { deployTokens.value = [] }
  finally { loadingTokens.value = false }
}

async function createDeployToken() {
  if (!tokenForm.value.name) return
  creatingToken.value = true
  newTokenValue.value = ''
  try {
    const data = await $api<{ token: string; id: string }>('/deploy-tokens', {
      method: 'POST',
      body: { name: tokenForm.value.name, scopes: tokenForm.value.scopes }
    })
    newTokenValue.value = data.token
    toast.add({ title: 'Token created - copy it now!', color: 'warning' })
    tokenForm.value = { name: '', scopes: ['deploy:*'] }
    await loadDeployTokens()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to create token', description: err.data?.error, color: 'error' })
  } finally { creatingToken.value = false }
}

async function deleteDeployToken(token: DeployToken) {
  if (!confirm(`Delete token "${token.name}"? Any CI/CD pipelines using this token will stop working.`)) return
  deletingToken.value = token.id
  try {
    await $api(`/deploy-tokens/${token.id}`, { method: 'DELETE' })
    toast.add({ title: 'Token deleted', color: 'success' })
    await loadDeployTokens()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Failed to delete token', description: err.data?.error, color: 'error' })
  } finally { deletingToken.value = null }
}

// ============================================================================
// Backup functionality
// ============================================================================

const backups = ref<BackupItem[]>([])
const loadingBackups = ref(false)
const creatingBackup = ref(false)
const restoringBackup = ref<string | null>(null)
const backupError = ref('')

// Backup progress tracking
const backupProgress = ref({
  step: 0,
  message: '',
  steps: [
    { icon: 'i-heroicons-circle-stack', label: 'Backing up database...' },
    { icon: 'i-heroicons-cog-6-tooth', label: 'Backing up configuration...' },
    { icon: 'i-heroicons-globe-alt', label: 'Backing up static sites...' },
    { icon: 'i-heroicons-archive-box', label: 'Exporting container volumes...' },
    { icon: 'i-heroicons-archive-box-arrow-down', label: 'Compressing backup...' }
  ]
})

// Backup options
const backupOptions = ref({
  includeVolumes: true,
  includeBuilds: false
})

// Restore options
const restoreOptions = ref({
  restoreDatabase: true,
  restoreConfig: true,
  restoreApps: true,
  restoreVolumes: true
})

// Delete confirmation modal
const deleteModalOpen = ref(false)
const backupToDelete = ref<BackupItem | null>(null)
const deletingBackup = ref(false)

// Restore confirmation modal
const restoreModalOpen = ref(false)
const backupToRestore = ref<BackupItem | null>(null)

const loadBackups = async () => {
  loadingBackups.value = true
  backupError.value = ''
  try {
    backups.value = await $api<BackupItem[]>('/backups')
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    backupError.value = err.data?.error || 'Failed to load backups'
  } finally {
    loadingBackups.value = false
  }
}

// Progress simulation for backup
let progressInterval: ReturnType<typeof setInterval> | null = null

const startProgressSimulation = () => {
  backupProgress.value.step = 0
  backupProgress.value.message = backupProgress.value.steps[0]?.label ?? ''

  progressInterval = setInterval(() => {
    if (backupProgress.value.step < backupProgress.value.steps.length - 1) {
      backupProgress.value.step++
      backupProgress.value.message = backupProgress.value.steps[backupProgress.value.step]?.label ?? ''
    }
  }, 2000) // Move to next step every 2 seconds
}

const stopProgressSimulation = () => {
  if (progressInterval) {
    clearInterval(progressInterval)
    progressInterval = null
  }
}

const createBackup = async () => {
  creatingBackup.value = true
  backupError.value = ''
  startProgressSimulation()

  try {
    const result = await $api<BackupItem>('/backups', {
      method: 'POST',
      body: {
        include_volumes: backupOptions.value.includeVolumes,
        include_builds: backupOptions.value.includeBuilds
      }
    })
    stopProgressSimulation()
    backupProgress.value.message = 'Backup complete!'

    toast.add({
      title: 'Backup created successfully',
      description: `${result.size_human} - ID: ${result.id}`,
      color: 'success'
    })
    await loadBackups()
  } catch (e: unknown) {
    stopProgressSimulation()
    const err = e as { data?: { error?: string } }
    backupError.value = err.data?.error || 'Failed to create backup'
    toast.add({ title: 'Backup failed', description: backupError.value, color: 'error' })
  } finally {
    creatingBackup.value = false
  }
}

const openRestoreModal = (backup: BackupItem) => {
  backupToRestore.value = backup
  restoreModalOpen.value = true
}

const confirmRestoreBackup = async () => {
  if (!backupToRestore.value) return

  restoringBackup.value = backupToRestore.value.id
  restoreModalOpen.value = false
  backupError.value = ''

  try {
    const result = await $api<RestoreResult>(`/backups/${backupToRestore.value.id}/restore`, {
      method: 'POST',
      body: {
        restore_database: restoreOptions.value.restoreDatabase,
        restore_config: restoreOptions.value.restoreConfig,
        restore_apps: restoreOptions.value.restoreApps,
        restore_volumes: restoreOptions.value.restoreVolumes
      }
    })

    let description = 'Restored: '
    const items = []
    if (result.database) items.push('database')
    if (result.config_files?.length) items.push(`${result.config_files.length} config files`)
    if (result.static_sites?.length) items.push(`${result.static_sites.length} sites`)
    if (result.volumes?.length) items.push(`${result.volumes.length} volumes`)
    description += items.join(', ')

    toast.add({
      title: 'Restore completed',
      description,
      color: 'success'
    })

    if (result.warnings?.length) {
      toast.add({
        title: 'Restore warnings',
        description: result.warnings.join(', '),
        color: 'warning'
      })
    }

    // Suggest restart
    toast.add({
      title: 'Restart recommended',
      description: 'Please restart basepod for all changes to take effect.',
      color: 'info'
    })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    backupError.value = err.data?.error || 'Failed to restore backup'
    toast.add({ title: 'Restore failed', description: backupError.value, color: 'error' })
  } finally {
    restoringBackup.value = null
    backupToRestore.value = null
  }
}

const openDeleteModal = (backup: BackupItem) => {
  backupToDelete.value = backup
  deleteModalOpen.value = true
}

const confirmDeleteBackup = async () => {
  if (!backupToDelete.value) return

  deletingBackup.value = true
  try {
    await $api(`/backups/${backupToDelete.value.id}`, { method: 'DELETE' })
    toast.add({ title: 'Backup deleted', color: 'success' })
    deleteModalOpen.value = false
    backupToDelete.value = null
    await loadBackups()
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    toast.add({ title: 'Delete failed', description: err.data?.error, color: 'error' })
  } finally {
    deletingBackup.value = false
  }
}

const downloadBackup = (backup: BackupItem) => {
  window.open(`/api/backups/${backup.id}/download`, '_blank')
}

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleString()
}
</script>

<template>
  <div>
    <div class="mb-6">
      <h1 class="text-2xl font-bold">Settings</h1>
      <p class="text-gray-500">Manage your Basepod configuration</p>
    </div>

    <UTabs :items="tabs" :default-value="defaultTab" class="w-full">
      <!-- General Tab -->
      <template #general>
        <div class="space-y-6 py-4">
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

              <UAlert v-if="domainError" color="error" variant="soft" :title="domainError" />
              <UAlert v-if="domainSuccess" color="success" variant="soft" title="Domain settings saved successfully" />

              <UButton type="submit" :loading="savingDomain">
                Save Domain Settings
              </UButton>
            </form>
          </UCard>

        </div>
      </template>

      <!-- Users Tab -->
      <template #users>
        <div class="space-y-6 py-4">
          <UCard>
            <template #header>
              <div class="flex items-center justify-between">
                <h3 class="font-semibold">Team Members</h3>
                <UButton size="sm" @click="showInviteForm = !showInviteForm; loadUsers()">
                  <UIcon name="i-heroicons-user-plus" class="mr-1" />
                  Invite User
                </UButton>
              </div>
            </template>

            <!-- Invite Form -->
            <div v-if="showInviteForm" class="mb-6 p-4 bg-(--ui-bg-muted) rounded-lg space-y-3">
              <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
                <UFormField label="Email" class="md:col-span-2">
                  <UInput v-model="inviteEmail" type="email" placeholder="user@example.com" />
                </UFormField>
                <UFormField label="Role">
                  <USelect v-model="inviteRole" :items="roleOptions.map(r => ({ label: r.label, value: r.value }))" />
                </UFormField>
              </div>
              <div class="flex gap-2">
                <UButton :loading="inviting" @click="inviteUser">Send Invite</UButton>
                <UButton variant="ghost" @click="showInviteForm = false">Cancel</UButton>
              </div>

              <!-- Invite Result -->
              <UAlert v-if="inviteResult" color="warning" variant="soft" title="Share this invite link">
                <template #description>
                  <code class="text-sm break-all">{{ inviteResult.invite_url }}</code>
                </template>
              </UAlert>
            </div>

            <!-- Users List -->
            <div v-if="loadingUsers" class="flex justify-center py-8">
              <UIcon name="i-heroicons-arrow-path" class="animate-spin text-2xl" />
            </div>

            <div v-else-if="users.length === 0" class="text-center py-8 text-gray-500">
              <UIcon name="i-heroicons-users" class="text-4xl mb-2" />
              <p>No users yet</p>
            </div>

            <div v-else class="divide-y divide-gray-200 dark:divide-gray-800">
              <div v-for="user in users" :key="user.id" class="py-3 flex items-center justify-between">
                <div class="flex items-center gap-3">
                  <div class="w-8 h-8 rounded-full bg-primary-100 dark:bg-primary-900 flex items-center justify-center">
                    <span class="text-sm font-medium text-primary-600 dark:text-primary-400">
                      {{ user.email.charAt(0).toUpperCase() }}
                    </span>
                  </div>
                  <div>
                    <p class="font-medium text-sm">{{ user.email }}</p>
                    <p class="text-xs text-gray-500">
                      Joined {{ new Date(user.created_at).toLocaleDateString() }}
                      <span v-if="user.last_login_at"> · Last login {{ new Date(user.last_login_at).toLocaleDateString() }}</span>
                    </p>
                  </div>
                </div>
                <div class="flex items-center gap-2">
                  <USelect
                    :model-value="user.role"
                    :items="roleOptions.map(r => ({ label: r.label, value: r.value }))"
                    size="xs"
                    :loading="updatingRole === user.id"
                    @update:model-value="(val: string) => updateUserRole(user.id, val)"
                  />
                  <UButton
                    icon="i-heroicons-trash"
                    variant="ghost"
                    color="error"
                    size="xs"
                    :loading="deletingUser === user.id"
                    @click="deleteUser(user)"
                  />
                </div>
              </div>
            </div>
          </UCard>

          <!-- Role Descriptions -->
          <UCard>
            <template #header>
              <h3 class="font-semibold">Roles</h3>
            </template>
            <div class="space-y-3">
              <div v-for="role in roleOptions" :key="role.value" class="flex items-start gap-3">
                <UBadge :color="role.value === 'admin' ? 'error' : role.value === 'deployer' ? 'warning' : 'info'" variant="soft" size="sm">
                  {{ role.label }}
                </UBadge>
                <span class="text-sm text-gray-500">{{ role.description }}</span>
              </div>
            </div>
          </UCard>
        </div>
      </template>

      <!-- Security Tab -->
      <template #security>
        <div class="space-y-6 py-4">
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
                  placeholder="Enter new password (min 8 characters)"
                />
              </UFormField>

              <UFormField label="Confirm New Password">
                <UInput
                  v-model="passwordForm.confirmPassword"
                  type="password"
                  placeholder="Confirm new password"
                />
              </UFormField>

              <UAlert v-if="passwordError" color="error" variant="soft" :title="passwordError" />
              <UAlert v-if="passwordSuccess" color="success" variant="soft" title="Password changed successfully" />

              <UButton type="submit" :loading="changingPassword">
                Change Password
              </UButton>
            </form>
          </UCard>

          <!-- Deploy Tokens -->
          <UCard>
            <template #header>
              <div class="flex items-center justify-between">
                <h3 class="font-semibold">Deploy Tokens</h3>
                <UButton size="sm" @click="showTokenForm = !showTokenForm; loadDeployTokens()">
                  <UIcon name="i-heroicons-plus" class="mr-1" />
                  Create Token
                </UButton>
              </div>
            </template>

            <!-- Create Token Form -->
            <div v-if="showTokenForm" class="mb-6 p-4 bg-(--ui-bg-muted) rounded-lg space-y-3">
              <UFormField label="Token Name" help="A descriptive name for this token (e.g., 'GitHub Actions')">
                <UInput v-model="tokenForm.name" placeholder="my-ci-token" />
              </UFormField>
              <div class="flex gap-2">
                <UButton :loading="creatingToken" @click="createDeployToken">Create Token</UButton>
                <UButton variant="ghost" @click="showTokenForm = false">Cancel</UButton>
              </div>

              <!-- New Token Display (only shown once) -->
              <UAlert v-if="newTokenValue" color="warning" variant="soft" title="Save this token - it won't be shown again!">
                <template #description>
                  <code class="text-sm break-all select-all">{{ newTokenValue }}</code>
                </template>
              </UAlert>
            </div>

            <!-- Tokens List -->
            <div v-if="loadingTokens" class="flex justify-center py-6">
              <UIcon name="i-heroicons-arrow-path" class="animate-spin text-2xl" />
            </div>

            <div v-else-if="deployTokens.length === 0 && !showTokenForm" class="text-center py-6 text-gray-500 text-sm">
              <p>No deploy tokens. Create one for CI/CD integration.</p>
            </div>

            <div v-else-if="deployTokens.length > 0" class="divide-y divide-gray-200 dark:divide-gray-800">
              <div v-for="token in deployTokens" :key="token.id" class="py-3 flex items-center justify-between">
                <div>
                  <p class="font-medium text-sm">{{ token.name }}</p>
                  <p class="text-xs text-gray-500">
                    <code>{{ token.prefix }}...</code>
                    · Created {{ new Date(token.created_at).toLocaleDateString() }}
                    <span v-if="token.last_used_at"> · Last used {{ new Date(token.last_used_at).toLocaleDateString() }}</span>
                    <span v-if="token.expires_at"> · Expires {{ new Date(token.expires_at).toLocaleDateString() }}</span>
                  </p>
                  <div class="flex gap-1 mt-1">
                    <UBadge v-for="scope in token.scopes" :key="scope" color="neutral" variant="soft" size="xs">{{ scope }}</UBadge>
                  </div>
                </div>
                <UButton
                  icon="i-heroicons-trash"
                  variant="ghost"
                  color="error"
                  size="xs"
                  :loading="deletingToken === token.id"
                  @click="deleteDeployToken(token)"
                />
              </div>
            </div>
          </UCard>
        </div>
      </template>

      <!-- Notifications Tab -->
      <template #notifications>
        <div class="space-y-6 py-4">
          <UCard>
            <template #header>
              <div class="flex items-center justify-between">
                <h3 class="font-semibold">Notification Hooks</h3>
                <UButton size="sm" @click="showNotificationForm = !showNotificationForm; loadNotifications()">
                  <UIcon name="i-heroicons-plus" class="mr-1" />
                  Add Hook
                </UButton>
              </div>
            </template>

            <!-- Create Notification Form -->
            <div v-if="showNotificationForm" class="mb-6 p-4 bg-(--ui-bg-muted) rounded-lg space-y-4">
              <UFormField label="Name">
                <UInput v-model="notificationForm.name" placeholder="My Slack notifications" />
              </UFormField>

              <UFormField label="Type">
                <USelect
                  v-model="notificationForm.type"
                  :items="[
                    { label: 'Webhook (HTTP POST)', value: 'webhook' },
                    { label: 'Slack', value: 'slack' },
                    { label: 'Discord', value: 'discord' }
                  ]"
                />
              </UFormField>

              <UFormField v-if="notificationForm.type === 'webhook'" label="Webhook URL">
                <UInput v-model="notificationForm.webhook_url" placeholder="https://example.com/webhook" />
              </UFormField>
              <UFormField v-if="notificationForm.type === 'slack'" label="Slack Webhook URL">
                <UInput v-model="notificationForm.slack_webhook_url" placeholder="https://hooks.slack.com/services/..." />
              </UFormField>
              <UFormField v-if="notificationForm.type === 'discord'" label="Discord Webhook URL">
                <UInput v-model="notificationForm.discord_webhook_url" placeholder="https://discord.com/api/webhooks/..." />
              </UFormField>

              <UFormField label="Events">
                <div class="flex flex-wrap gap-2">
                  <UCheckbox
                    v-for="evt in availableEvents"
                    :key="evt.value"
                    :model-value="notificationForm.events.includes(evt.value)"
                    :label="evt.label"
                    @update:model-value="(checked: boolean | 'indeterminate') => {
                      if (checked === true) notificationForm.events.push(evt.value)
                      else notificationForm.events = notificationForm.events.filter(e => e !== evt.value)
                    }"
                  />
                </div>
              </UFormField>

              <div class="flex gap-2">
                <UButton :loading="savingNotification" @click="createNotification">Create Hook</UButton>
                <UButton variant="ghost" @click="showNotificationForm = false">Cancel</UButton>
              </div>
            </div>

            <!-- Notifications List -->
            <div v-if="loadingNotifications" class="flex justify-center py-6">
              <UIcon name="i-heroicons-arrow-path" class="animate-spin text-2xl" />
            </div>

            <div v-else-if="notifications.length === 0 && !showNotificationForm" class="text-center py-6 text-gray-500 text-sm">
              <UIcon name="i-heroicons-bell-slash" class="text-4xl mb-2" />
              <p>No notification hooks configured</p>
              <p class="text-xs mt-1">Get notified about deploys, health checks, and more</p>
            </div>

            <div v-else-if="notifications.length > 0" class="divide-y divide-gray-200 dark:divide-gray-800">
              <div v-for="notif in notifications" :key="notif.id" class="py-3">
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-3">
                    <UIcon
                      :name="notif.type === 'slack' ? 'i-heroicons-chat-bubble-left' : notif.type === 'discord' ? 'i-heroicons-chat-bubble-bottom-center-text' : 'i-heroicons-globe-alt'"
                      class="w-5 h-5 text-gray-400"
                    />
                    <div>
                      <div class="flex items-center gap-2">
                        <p class="font-medium text-sm">{{ notif.name }}</p>
                        <UBadge :color="notif.enabled ? 'success' : 'neutral'" variant="soft" size="xs">
                          {{ notif.enabled ? 'Active' : 'Disabled' }}
                        </UBadge>
                        <UBadge color="info" variant="soft" size="xs">{{ notif.type }}</UBadge>
                      </div>
                      <div class="flex gap-1 mt-1">
                        <UBadge v-for="evt in notif.events" :key="evt" color="neutral" variant="soft" size="xs">{{ evt }}</UBadge>
                      </div>
                    </div>
                  </div>
                  <div class="flex items-center gap-1">
                    <UButton
                      :icon="notif.enabled ? 'i-heroicons-pause' : 'i-heroicons-play'"
                      variant="ghost"
                      size="xs"
                      @click="toggleNotification(notif)"
                    />
                    <UButton
                      icon="i-heroicons-paper-airplane"
                      variant="ghost"
                      color="primary"
                      size="xs"
                      :loading="testingNotification === notif.id"
                      @click="testNotification(notif.id)"
                    />
                    <UButton
                      icon="i-heroicons-trash"
                      variant="ghost"
                      color="error"
                      size="xs"
                      :loading="deletingNotification === notif.id"
                      @click="deleteNotification(notif)"
                    />
                  </div>
                </div>
              </div>
            </div>
          </UCard>
        </div>
      </template>

      <!-- Backup Tab -->
      <template #backup>
        <div class="space-y-6 py-4">
          <!-- Create Backup -->
          <UCard>
            <template #header>
              <div class="flex items-center justify-between">
                <h3 class="font-semibold">Create Backup</h3>
              </div>
            </template>

            <div class="space-y-4">
              <!-- Progress indicator when backup is running -->
              <div v-if="creatingBackup" class="space-y-4">
                <div class="flex items-center gap-3 p-4 bg-primary-50 dark:bg-primary-950 rounded-lg">
                  <div class="relative">
                    <UIcon
                      :name="backupProgress.steps[backupProgress.step]?.icon || 'i-heroicons-arrow-path'"
                      class="text-2xl text-primary-500 animate-pulse"
                    />
                  </div>
                  <div class="flex-1">
                    <p class="font-medium text-primary-700 dark:text-primary-300">
                      {{ backupProgress.message }}
                    </p>
                    <p class="text-sm text-primary-600 dark:text-primary-400">
                      Step {{ backupProgress.step + 1 }} of {{ backupProgress.steps.length }}
                    </p>
                  </div>
                </div>

                <!-- Progress steps -->
                <div class="flex items-center justify-between px-2">
                  <div
                    v-for="(step, index) in backupProgress.steps"
                    :key="index"
                    class="flex flex-col items-center gap-1"
                  >
                    <div
                      class="w-8 h-8 rounded-full flex items-center justify-center transition-all duration-300"
                      :class="index <= backupProgress.step
                        ? 'bg-primary-500 text-white'
                        : 'bg-gray-200 dark:bg-gray-700 text-gray-400'"
                    >
                      <UIcon
                        v-if="index < backupProgress.step"
                        name="i-heroicons-check"
                        class="text-sm"
                      />
                      <UIcon
                        v-else-if="index === backupProgress.step"
                        name="i-heroicons-arrow-path"
                        class="text-sm animate-spin"
                      />
                      <span v-else class="text-xs">{{ index + 1 }}</span>
                    </div>
                  </div>
                </div>

                <!-- Progress bar -->
                <div class="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                  <div
                    class="bg-primary-500 h-2 rounded-full transition-all duration-500"
                    :style="{ width: `${((backupProgress.step + 1) / backupProgress.steps.length) * 100}%` }"
                  />
                </div>
              </div>

              <!-- Normal state (not backing up) -->
              <template v-else>
                <p class="text-sm text-gray-500">
                  Create a backup of your database, configuration, static sites, and container volumes.
                </p>

                <div class="flex flex-col gap-2">
                  <UCheckbox v-model="backupOptions.includeVolumes" label="Include container volumes" />
                  <UCheckbox v-model="backupOptions.includeBuilds" label="Include build sources (Dockerfiles)" />
                </div>

                <UButton
                  color="primary"
                  @click="createBackup"
                >
                  <template #leading>
                    <UIcon name="i-heroicons-cloud-arrow-up" />
                  </template>
                  Create Backup
                </UButton>
              </template>
            </div>
          </UCard>

          <!-- Backup List -->
          <UCard>
            <template #header>
              <div class="flex items-center justify-between">
                <h3 class="font-semibold">Available Backups</h3>
                <UButton
                  variant="ghost"
                  size="sm"
                  :loading="loadingBackups"
                  @click="loadBackups"
                >
                  <UIcon name="i-heroicons-arrow-path" />
                </UButton>
              </div>
            </template>

            <UAlert v-if="backupError" color="error" variant="soft" :title="backupError" class="mb-4" />

            <div v-if="loadingBackups" class="flex justify-center py-8">
              <UIcon name="i-heroicons-arrow-path" class="animate-spin text-2xl" />
            </div>

            <div v-else-if="backups.length === 0" class="text-center py-8 text-gray-500">
              <UIcon name="i-heroicons-archive-box" class="text-4xl mb-2" />
              <p>No backups found</p>
              <p class="text-sm">Create your first backup above</p>
            </div>

            <div v-else class="divide-y divide-gray-200 dark:divide-gray-800">
              <div
                v-for="backup in backups"
                :key="backup.id"
                class="py-4 first:pt-0 last:pb-0"
              >
                <div class="flex items-start justify-between">
                  <div class="flex-1">
                    <div class="flex items-center gap-2">
                      <span class="font-mono font-medium">{{ backup.id }}</span>
                      <UBadge color="neutral" variant="soft" size="xs">
                        {{ backup.size_human }}
                      </UBadge>
                    </div>
                    <p class="text-sm text-gray-500 mt-1">
                      {{ formatDate(backup.created_at) }}
                    </p>
                    <div class="flex flex-wrap gap-1 mt-2">
                      <UBadge v-if="backup.contents.database" color="primary" variant="soft" size="xs">
                        Database
                      </UBadge>
                      <UBadge v-if="backup.contents.config" color="info" variant="soft" size="xs">
                        Config
                      </UBadge>
                      <UBadge
                        v-if="backup.contents.static_sites?.length"
                        color="success"
                        variant="soft"
                        size="xs"
                      >
                        {{ backup.contents.static_sites.length }} Sites
                      </UBadge>
                      <UBadge
                        v-if="backup.contents.volumes?.length"
                        color="warning"
                        variant="soft"
                        size="xs"
                      >
                        {{ backup.contents.volumes.length }} Volumes
                      </UBadge>
                    </div>
                  </div>

                  <div class="flex gap-1">
                    <UButton
                      variant="ghost"
                      size="sm"
                      color="primary"
                      :loading="restoringBackup === backup.id"
                      @click="openRestoreModal(backup)"
                    >
                      <UIcon name="i-heroicons-arrow-path" />
                      Restore
                    </UButton>
                    <UButton
                      variant="ghost"
                      size="sm"
                      @click="downloadBackup(backup)"
                    >
                      <UIcon name="i-heroicons-arrow-down-tray" />
                    </UButton>
                    <UButton
                      variant="ghost"
                      size="sm"
                      color="error"
                      @click="openDeleteModal(backup)"
                    >
                      <UIcon name="i-heroicons-trash" />
                    </UButton>
                  </div>
                </div>
              </div>
            </div>
          </UCard>

          <!-- Restore Options -->
          <UCard>
            <template #header>
              <h3 class="font-semibold">Restore Options</h3>
            </template>

            <p class="text-sm text-gray-500 mb-4">
              Configure what to restore when using the Restore button above.
            </p>

            <div class="flex flex-col gap-2">
              <UCheckbox v-model="restoreOptions.restoreDatabase" label="Restore database" />
              <UCheckbox v-model="restoreOptions.restoreConfig" label="Restore configuration files" />
              <UCheckbox v-model="restoreOptions.restoreApps" label="Restore static sites" />
              <UCheckbox v-model="restoreOptions.restoreVolumes" label="Restore container volumes" />
            </div>
          </UCard>
        </div>
      </template>

      <!-- System Tab -->
      <template #system>
        <div class="space-y-6 py-4">
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

              <UAlert v-if="updateMessage" color="success" variant="soft" :title="updateMessage" />
              <UAlert v-if="updateError" color="error" variant="soft" :title="updateError" />

              <div class="flex gap-2">
                <UButton variant="soft" :loading="checkingVersion" @click="checkVersion">
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
                  <p class="font-medium">Basepod</p>
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
              <UAlert v-if="pruneResult" color="success" variant="soft" title="Prune completed">
                <pre class="text-xs mt-2 whitespace-pre-wrap">{{ pruneResult }}</pre>
              </UAlert>

              <UAlert v-if="pruneError" color="error" variant="soft" :title="pruneError" />

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
      </template>
    </UTabs>

    <!-- Delete Backup Confirmation Modal -->
    <ModalsConfirmModal
      v-model:open="deleteModalOpen"
      title="Delete Backup"
      :description="`Are you sure you want to delete backup '${backupToDelete?.id}'? This action cannot be undone.`"
      confirm-label="Delete"
      confirm-color="error"
      :loading="deletingBackup"
      @confirm="confirmDeleteBackup"
      @cancel="deleteModalOpen = false"
    />

    <!-- Restore Backup Confirmation Modal -->
    <ModalsConfirmModal
      v-model:open="restoreModalOpen"
      title="Restore Backup"
      confirm-label="Restore"
      confirm-color="warning"
      :loading="restoringBackup === backupToRestore?.id"
      @confirm="confirmRestoreBackup"
      @cancel="restoreModalOpen = false"
    >
      <div class="space-y-3">
        <p class="text-gray-600 dark:text-gray-300">
          You are about to restore backup <strong class="font-mono">{{ backupToRestore?.id }}</strong>.
        </p>

        <UAlert color="warning" variant="soft" title="This will overwrite existing data">
          <template #description>
            <ul class="list-disc list-inside mt-2 space-y-1 text-sm">
              <li v-if="restoreOptions.restoreDatabase">
                <strong>Database:</strong> All app metadata and settings will be replaced
              </li>
              <li v-if="restoreOptions.restoreConfig">
                <strong>Configuration:</strong> basepod.yaml and system config will be overwritten
              </li>
              <li v-if="restoreOptions.restoreApps && backupToRestore?.contents.static_sites?.length">
                <strong>Static Sites:</strong> {{ backupToRestore?.contents.static_sites?.length }} site(s) will be restored
              </li>
              <li v-if="restoreOptions.restoreVolumes && backupToRestore?.contents.volumes?.length">
                <strong>Volumes:</strong> {{ backupToRestore?.contents.volumes?.length }} container volume(s) will be replaced
              </li>
            </ul>
          </template>
        </UAlert>

        <p class="text-sm text-gray-500">
          A server restart is recommended after restore for all changes to take effect.
        </p>
      </div>
    </ModalsConfirmModal>
  </div>
</template>
