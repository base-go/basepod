<script setup lang="ts">
interface Props {
  title?: string
  description?: string
  confirmLabel?: string
  cancelLabel?: string
  confirmColor?: 'primary' | 'error' | 'warning' | 'success' | 'neutral'
  loading?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  title: 'Confirm Action',
  description: 'Are you sure you want to proceed?',
  confirmLabel: 'Confirm',
  cancelLabel: 'Cancel',
  confirmColor: 'primary',
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
</script>

<template>
  <UModal v-model:open="open" :title="props.title" :description="props.description">
    <template #body>
      <slot />
    </template>

    <template #footer>
      <UButton
        :label="props.cancelLabel"
        variant="outline"
        :disabled="props.loading"
        @click="handleCancel"
      />
      <UButton
        :label="props.confirmLabel"
        :color="props.confirmColor"
        :loading="props.loading"
        @click="handleConfirm"
      />
    </template>
  </UModal>
</template>
