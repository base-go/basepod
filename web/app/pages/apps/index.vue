<script setup lang="ts">
import type { AppsResponse, App } from '~/types'
import type { TableColumn } from '@nuxt/ui'

definePageMeta({
  title: 'Apps'
})

const toast = useToast()
const { data: apps, refresh } = await useApiFetch<AppsResponse>('/apps')

const isCreateModalOpen = ref(false)
const isDeployModalOpen = ref(false)
const selectedApp = ref<App | null>(null)

function getErrorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'data' in error) {
    const data = (error as { data?: { error?: string } }).data
    if (data?.error) return data.error
  }
  return 'An unexpected error occurred'
}

async function startApp(app: App) {
  try {
    await $api(`/apps/${app.id}/start`, { method: 'POST' })
    toast.add({ title: `${app.name} started`, color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to start app', description: getErrorMessage(error), color: 'error' })
  }
}

async function stopApp(app: App) {
  try {
    await $api(`/apps/${app.id}/stop`, { method: 'POST' })
    toast.add({ title: `${app.name} stopped`, color: 'warning' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to stop app', description: getErrorMessage(error), color: 'error' })
  }
}

async function deleteApp(app: App) {
  if (!confirm(`Delete ${app.name}?`)) return
  try {
    await $api(`/apps/${app.id}`, { method: 'DELETE' })
    toast.add({ title: `${app.name} deleted`, color: 'success' })
    refresh()
  } catch (error) {
    toast.add({ title: 'Failed to delete app', description: getErrorMessage(error), color: 'error' })
  }
}

async function deployApp(app: App) {
  // If app has an image, deploy directly without modal
  if (app.image) {
    try {
      await $api(`/apps/${app.id}/deploy`, {
        method: 'POST',
        body: { image: app.image }
      })
      toast.add({ title: `Deploying ${app.name}...`, color: 'success' })
      refresh()
    } catch (error) {
      toast.add({ title: 'Failed to deploy', description: getErrorMessage(error), color: 'error' })
    }
  } else {
    // No image defined, show modal
    selectedApp.value = app
    isDeployModalOpen.value = true
  }
}

const columns: TableColumn<App>[] = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'status', header: 'Status' },
  { accessorKey: 'domain', header: 'Domain' },
  { accessorKey: 'image', header: 'Image' },
  { id: 'actions', header: '' }
]
</script>

<template>
  <div>
    <!-- Header Actions -->
    <div class="flex items-center justify-between mb-6">
      <div>
        <h2 class="text-xl font-semibold">Applications</h2>
        <p class="text-gray-500 dark:text-gray-400">Manage your deployed applications</p>
      </div>
      <UButton icon="i-heroicons-plus" @click="isCreateModalOpen = true">
        Create App
      </UButton>
    </div>

    <!-- Apps Table -->
    <UCard>
      <UTable :columns="columns" :data="apps?.apps || []">
        <template #name-cell="{ row }">
          <NuxtLink :to="`/apps/${row.original.id}`" class="flex items-center gap-2 hover:text-primary-500">
            <UIcon name="i-heroicons-cube" class="w-5 h-5 text-gray-400" />
            <span class="font-medium">{{ row.original.name }}</span>
          </NuxtLink>
        </template>

        <template #status-cell="{ row }">
          <UBadge
            :color="row.original.status === 'running' ? 'success' : row.original.status === 'stopped' ? 'warning' : 'neutral'"
          >
            {{ row.original.status }}
          </UBadge>
        </template>

        <template #domain-cell="{ row }">
          <a
            v-if="row.original.domain"
            :href="`http://${row.original.domain}`"
            target="_blank"
            class="text-primary-500 hover:underline"
          >
            {{ row.original.domain }}
          </a>
          <span v-else class="text-gray-400">-</span>
        </template>

        <template #image-cell="{ row }">
          <code v-if="row.original.image" class="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">
            {{ row.original.image }}
          </code>
          <span v-else class="text-gray-400">Not deployed</span>
        </template>

        <template #actions-cell="{ row }">
          <div class="flex items-center justify-end gap-2">
            <UButton
              v-if="row.original.status !== 'running'"
              icon="i-heroicons-play"
              variant="ghost"
              color="success"
              size="sm"
              @click="startApp(row.original)"
            />
            <UButton
              v-if="row.original.status === 'running'"
              icon="i-heroicons-stop"
              variant="ghost"
              color="warning"
              size="sm"
              @click="stopApp(row.original)"
            />
            <UButton
              icon="i-heroicons-arrow-up-tray"
              variant="ghost"
              size="sm"
              @click="deployApp(row.original)"
            />
            <UButton
              icon="i-heroicons-trash"
              variant="ghost"
              color="error"
              size="sm"
              @click="deleteApp(row.original)"
            />
          </div>
        </template>
      </UTable>

      <div v-if="!apps?.apps?.length" class="text-center py-12">
        <UIcon name="i-heroicons-inbox" class="w-16 h-16 mx-auto mb-4 text-gray-300" />
        <h3 class="text-lg font-medium mb-2">No apps yet</h3>
        <p class="text-gray-500 mb-4">Create your first application to get started</p>
        <UButton @click="isCreateModalOpen = true">Create App</UButton>
      </div>
    </UCard>

    <AppsCreateAppModal v-model:open="isCreateModalOpen" @created="refresh()" />
    <AppsDeployAppModal v-model:open="isDeployModalOpen" :app="selectedApp" @deployed="refresh()" />
  </div>
</template>
