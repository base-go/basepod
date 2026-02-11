<script setup lang="ts">
const {
  title = 'Confirm Action',
  message = 'Are you sure you want to proceed?',
  confirmText = 'Confirm',
  cancelText = 'Cancel',
  confirmColor = 'error',
  icon = 'i-heroicons-exclamation-triangle'
} = defineProps<{
  title?: string
  message?: string
  confirmText?: string
  cancelText?: string
  confirmColor?: 'primary' | 'error' | 'warning' | 'success' | 'neutral'
  icon?: string
}>()

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })

function handleConfirm() {
  emit('confirm')
  isOpen.value = false
}

function handleCancel() {
  emit('cancel')
  isOpen.value = false
}
</script>

<template>
  <UModal v-model:open="isOpen">
    <template #content>
      <div class="p-6">
        <div class="flex items-start gap-4">
          <div
            class="flex items-center justify-center w-12 h-12 rounded-full shrink-0"
            :class="{
              'bg-red-100 dark:bg-red-900/20': confirmColor === 'error',
              'bg-yellow-100 dark:bg-yellow-900/20': confirmColor === 'warning',
              'bg-primary-100 dark:bg-primary-900/20': confirmColor === 'primary',
              'bg-green-100 dark:bg-green-900/20': confirmColor === 'success',
              'bg-(--ui-bg-muted)': confirmColor === 'neutral'
            }"
          >
            <UIcon
              :name="icon"
              class="w-6 h-6"
              :class="{
                'text-red-600 dark:text-red-400': confirmColor === 'error',
                'text-yellow-600 dark:text-yellow-400': confirmColor === 'warning',
                'text-primary-600 dark:text-primary-400': confirmColor === 'primary',
                'text-green-600 dark:text-green-400': confirmColor === 'success',
                'text-gray-600 dark:text-gray-400': confirmColor === 'neutral'
              }"
            />
          </div>
          <div class="flex-1">
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ title }}
            </h3>
            <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
              {{ message }}
            </p>
          </div>
        </div>

        <div class="flex justify-end gap-3 mt-6">
          <UButton
            variant="outline"
            color="neutral"
            @click="handleCancel"
          >
            {{ cancelText }}
          </UButton>
          <UButton
            :color="confirmColor"
            @click="handleConfirm"
          >
            {{ confirmText }}
          </UButton>
        </div>
      </div>
    </template>
  </UModal>
</template>
