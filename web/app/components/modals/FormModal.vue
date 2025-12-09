<script setup lang="ts">
interface Props {
  title?: string
  description?: string
  submitLabel?: string
  cancelLabel?: string
  loading?: boolean
  fullscreen?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  title: 'Form',
  description: '',
  submitLabel: 'Save',
  cancelLabel: 'Cancel',
  loading: false,
  fullscreen: false
})

const open = defineModel<boolean>('open', { default: false })

const emit = defineEmits<{
  submit: []
  cancel: []
}>()

function handleSubmit() {
  emit('submit')
}

function handleCancel() {
  open.value = false
  emit('cancel')
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="props.title"
    :description="props.description"
    :fullscreen="props.fullscreen"
  >
    <slot name="trigger" />

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
        :label="props.submitLabel"
        :loading="props.loading"
        @click="handleSubmit"
      />
    </template>
  </UModal>
</template>
