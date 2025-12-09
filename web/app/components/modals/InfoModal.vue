<script setup lang="ts">
interface Props {
  title?: string
  description?: string
  closeLabel?: string
}

const props = withDefaults(defineProps<Props>(), {
  title: 'Information',
  description: '',
  closeLabel: 'Close'
})

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <UModal v-model:open="open" :title="props.title" :description="props.description">
    <slot name="trigger" />

    <template #body>
      <slot />
    </template>

    <template #footer>
      <UButton
        :label="props.closeLabel"
        @click="open = false"
      />
    </template>
  </UModal>
</template>
