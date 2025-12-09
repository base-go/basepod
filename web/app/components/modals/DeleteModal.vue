<script setup lang="ts">
interface Props {
  title?: string
  description?: string
  itemName?: string
  loading?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  title: 'Delete Item',
  description: 'This action cannot be undone.',
  itemName: '',
  loading: false
})

const open = defineModel<boolean>('open', { default: false })

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()

function handleConfirm() {
  emit('confirm')
}

function handleCancel() {
  open.value = false
  emit('cancel')
}

const displayDescription = computed(() => {
  if (props.itemName) {
    return `Are you sure you want to delete "${props.itemName}"? ${props.description}`
  }
  return props.description
})
</script>

<template>
  <UModal v-model:open="open" :title="props.title" :description="displayDescription">
    <slot />

    <template #footer>
      <UButton
        label="Cancel"
        variant="outline"
        :disabled="props.loading"
        @click="handleCancel"
      />
      <UButton
        label="Delete"
        color="error"
        :loading="props.loading"
        @click="handleConfirm"
      />
    </template>
  </UModal>
</template>
