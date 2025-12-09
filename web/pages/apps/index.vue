<script setup lang="ts">
definePageMeta({
  title: 'Apps'
})

const { data: apps, refresh } = await useFetch('/api/apps')

const isCreateModalOpen = ref(false)
const isDeployModalOpen = ref(false)
const selectedApp = ref<any>(null)

const newApp = ref({
  name: '',
  domain: '',
  port: 8080,
  enableSSL: true
})

const deployForm = ref({
  image: ''
})

async function createApp() {
  try {
    await $fetch('/api/apps', {
      method: 'POST',
      body: newApp.value
    })
    isCreateModalOpen.value = false
    newApp.value = { name: '', domain: '', port: 8080, enableSSL: true }
    refresh()
  } catch (error: any) {
    console.error('Failed to create app:', error)
  }
}

async function deployApp() {
  if (!selectedApp.value) return

  try {
    await $fetch(`/api/apps/${selectedApp.value.id}/deploy`, {
      method: 'POST',
      body: deployForm.value
    })
    isDeployModalOpen.value = false
    deployForm.value = { image: '' }
    refresh()
  } catch (error: any) {
    console.error('Failed to deploy:', error)
  }
}

async function startApp(app: any) {
  await $fetch(`/api/apps/${app.id}/start`, { method: 'POST' })
  refresh()
}

async function stopApp(app: any) {
  await $fetch(`/api/apps/${app.id}/stop`, { method: 'POST' })
  refresh()
}

async function deleteApp(app: any) {
  if (!confirm(`Delete ${app.name}?`)) return
  await $fetch(`/api/apps/${app.id}`, { method: 'DELETE' })
  refresh()
}

function openDeploy(app: any) {
  selectedApp.value = app
  isDeployModalOpen.value = true
}

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'status', label: 'Status' },
  { key: 'domain', label: 'Domain' },
  { key: 'image', label: 'Image' },
  { key: 'actions', label: '' }
]
</script>

<template>
  <div>
    <template #header>
      Apps
    </template>

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
      <UTable :columns="columns" :rows="apps?.apps || []">
        <template #name-data="{ row }">
          <div class="flex items-center gap-2">
            <UIcon name="i-heroicons-cube" class="w-5 h-5 text-gray-400" />
            <span class="font-medium">{{ row.name }}</span>
          </div>
        </template>

        <template #status-data="{ row }">
          <UBadge
            :color="row.status === 'running' ? 'success' : row.status === 'stopped' ? 'warning' : 'gray'"
          >
            {{ row.status }}
          </UBadge>
        </template>

        <template #domain-data="{ row }">
          <a
            v-if="row.domain"
            :href="`https://${row.domain}`"
            target="_blank"
            class="text-primary-500 hover:underline"
          >
            {{ row.domain }}
          </a>
          <span v-else class="text-gray-400">-</span>
        </template>

        <template #image-data="{ row }">
          <code v-if="row.image" class="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">
            {{ row.image }}
          </code>
          <span v-else class="text-gray-400">Not deployed</span>
        </template>

        <template #actions-data="{ row }">
          <div class="flex items-center justify-end gap-2">
            <UButton
              v-if="row.status !== 'running'"
              icon="i-heroicons-play"
              variant="ghost"
              color="success"
              size="sm"
              @click="startApp(row)"
            />
            <UButton
              v-if="row.status === 'running'"
              icon="i-heroicons-stop"
              variant="ghost"
              color="warning"
              size="sm"
              @click="stopApp(row)"
            />
            <UButton
              icon="i-heroicons-arrow-up-tray"
              variant="ghost"
              size="sm"
              @click="openDeploy(row)"
            />
            <UButton
              icon="i-heroicons-trash"
              variant="ghost"
              color="error"
              size="sm"
              @click="deleteApp(row)"
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

    <!-- Create App Modal -->
    <UModal v-model:open="isCreateModalOpen">
      <template #header>
        <h3 class="text-lg font-semibold">Create New App</h3>
      </template>

      <form @submit.prevent="createApp" class="space-y-4 p-4">
        <UFormField label="App Name" required>
          <UInput v-model="newApp.name" placeholder="my-app" />
        </UFormField>

        <UFormField label="Domain">
          <UInput v-model="newApp.domain" placeholder="my-app.example.com" />
        </UFormField>

        <UFormField label="Container Port">
          <UInput v-model.number="newApp.port" type="number" />
        </UFormField>

        <UFormField>
          <UCheckbox v-model="newApp.enableSSL" label="Enable SSL (HTTPS)" />
        </UFormField>

        <div class="flex justify-end gap-2 pt-4">
          <UButton variant="ghost" @click="isCreateModalOpen = false">Cancel</UButton>
          <UButton type="submit">Create</UButton>
        </div>
      </form>
    </UModal>

    <!-- Deploy Modal -->
    <UModal v-model:open="isDeployModalOpen">
      <template #header>
        <h3 class="text-lg font-semibold">Deploy {{ selectedApp?.name }}</h3>
      </template>

      <form @submit.prevent="deployApp" class="space-y-4 p-4">
        <UFormField label="Docker Image" required>
          <UInput v-model="deployForm.image" placeholder="nginx:latest" />
        </UFormField>

        <p class="text-sm text-gray-500">
          Enter a Docker image from Docker Hub or a private registry.
        </p>

        <div class="flex justify-end gap-2 pt-4">
          <UButton variant="ghost" @click="isDeployModalOpen = false">Cancel</UButton>
          <UButton type="submit" :loading="false">Deploy</UButton>
        </div>
      </form>
    </UModal>
  </div>
</template>
