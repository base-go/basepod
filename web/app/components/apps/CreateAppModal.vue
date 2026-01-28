<script setup lang="ts">
import type { ConfigResponse } from '~/composables/useApi'
import { getAppDomain } from '~/composables/useApi'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  created: []
}>()

const toast = useToast()
const { data: configData } = await useApiFetch<ConfigResponse>('/system/config')

const form = ref({
  name: '',
  persistentStorage: false
})

const isOpen = computed({
  get: () => props.open,
  set: (value) => emit('update:open', value)
})

// Auto-generate domain from app name using config
const autoDomain = computed(() => {
  if (!form.value.name) return ''
  const name = form.value.name.toLowerCase().replace(/[^a-z0-9-]/g, '-')
  if (configData.value?.domain) {
    return getAppDomain(name, configData.value.domain)
  }
  return `${name}.pod`
})

// Domain suffix for placeholder display
const domainPlaceholder = computed(() => {
  if (configData.value?.domain?.base) {
    return `app-name.${configData.value.domain.root}`
  }
  return `app-name${configData.value?.domain?.suffix || '.pod'}`
})

async function submit() {
  try {
    const body: Record<string, unknown> = {
      name: form.value.name
    }

    // Add persistent volume if enabled
    if (form.value.persistentStorage) {
      body.volumes = [{
        name: 'data',
        container_path: '/data'
      }]
    }

    await $api('/apps', {
      method: 'POST',
      body
    })
    isOpen.value = false
    form.value = { name: '', persistentStorage: false }
    toast.add({ title: 'App created successfully', color: 'success' })
    emit('created')
  } catch (error) {
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'An unexpected error occurred'
    toast.add({ title: 'Failed to create app', description: message, color: 'error' })
  }
}
</script>

<template>
  <UModal v-model:open="isOpen" title="Create New App">
    <template #body>
      <div class="space-y-4">
        <UFormField label="App Name" required>
          <UInput v-model="form.name" placeholder="my-app" autofocus />
        </UFormField>

        <UFormField label="Domain">
          <div class="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-800 rounded-md font-mono text-sm">
            <span class="text-gray-500">https://</span>
            <span>{{ autoDomain || domainPlaceholder }}</span>
          </div>
          <p class="text-xs text-gray-500 mt-1">Auto-assigned based on app name</p>
        </UFormField>

        <div class="flex items-center justify-between py-2">
          <div>
            <p class="font-medium text-sm">Persistent Storage</p>
            <p class="text-xs text-gray-500">Data persists across container restarts</p>
          </div>
          <USwitch v-model="form.persistentStorage" />
        </div>

        <div v-if="form.persistentStorage" class="px-3 py-2 bg-blue-50 dark:bg-blue-900/20 rounded-md text-sm">
          <p class="text-blue-700 dark:text-blue-300">
            <UIcon name="i-heroicons-information-circle" class="w-4 h-4 inline mr-1" />
            Volume <code class="bg-blue-100 dark:bg-blue-800 px-1 rounded">{{ form.name || 'app' }}-data</code> will be mounted at <code class="bg-blue-100 dark:bg-blue-800 px-1 rounded">/data</code>
          </p>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton variant="ghost" @click="isOpen = false">Cancel</UButton>
        <UButton :disabled="!form.name.trim()" @click="submit">Create</UButton>
      </div>
    </template>
  </UModal>
</template>
