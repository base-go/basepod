<script setup lang="ts">
import type { App } from '~/types'

const props = defineProps<{
  open: boolean
  app: App | null
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  deployed: []
}>()

const toast = useToast()

const form = ref({
  image: ''
})

const isOpen = computed({
  get: () => props.open,
  set: (value) => emit('update:open', value)
})

// Pre-fill form with app's current image when modal opens
watch(() => props.open, (open) => {
  if (open && props.app?.image) {
    form.value.image = props.app.image
  }
})

async function submit() {
  if (!props.app) return

  try {
    await $api(`/apps/${props.app.id}/deploy`, {
      method: 'POST',
      body: form.value
    })
    isOpen.value = false
    form.value = { image: '' }
    toast.add({ title: 'Deployment started', color: 'success' })
    emit('deployed')
  } catch (error) {
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'An unexpected error occurred'
    toast.add({ title: 'Failed to deploy', description: message, color: 'error' })
  }
}
</script>

<template>
  <UModal v-model:open="isOpen" :title="`Deploy ${app?.name}`">
    <template #body>
      <div class="space-y-4">
        <UFormField label="Docker Image" required>
          <UInput v-model="form.image" placeholder="nginx:latest" />
        </UFormField>

        <p class="text-sm text-gray-500">
          Enter a Docker image from Docker Hub or a private registry.
        </p>
      </div>
    </template>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton variant="ghost" @click="isOpen = false">Cancel</UButton>
        <UButton @click="submit">Deploy</UButton>
      </div>
    </template>
  </UModal>
</template>
