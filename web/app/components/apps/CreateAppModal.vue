<script setup lang="ts">
const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  created: []
}>()

const toast = useToast()

const form = ref({
  name: '',
  port: 8080,
  enableSSL: false
})

const isOpen = computed({
  get: () => props.open,
  set: (value) => emit('update:open', value)
})

// Auto-generate domain from app name
const autoDomain = computed(() => {
  if (!form.value.name) return ''
  return `${form.value.name.toLowerCase().replace(/[^a-z0-9-]/g, '-')}.pod`
})

async function submit() {
  try {
    await $api('/apps', {
      method: 'POST',
      body: form.value
    })
    isOpen.value = false
    form.value = { name: '', port: 8080, enableSSL: false }
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
          <UInput v-model="form.name" placeholder="my-app" />
        </UFormField>

        <UFormField label="Domain">
          <div class="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-800 rounded-md font-mono text-sm">
            <span class="text-gray-500">http://</span>
            <span>{{ autoDomain || 'app-name.pod' }}</span>
          </div>
          <p class="text-xs text-gray-500 mt-1">Auto-assigned based on app name</p>
        </UFormField>

        <UFormField label="Container Port">
          <UInput v-model.number="form.port" type="number" />
        </UFormField>
      </div>
    </template>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton variant="ghost" @click="isOpen = false">Cancel</UButton>
        <UButton @click="submit">Create</UButton>
      </div>
    </template>
  </UModal>
</template>
