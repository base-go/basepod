<script setup lang="ts">
interface Props {
  label: string
  value: number | string
  icon: string
  description?: string
  color?: 'primary' | 'success' | 'warning' | 'error' | 'info' | 'neutral'
  to?: string
}

withDefaults(defineProps<Props>(), {
  color: 'primary',
  description: undefined,
  to: undefined
})
</script>

<template>
  <component :is="to ? resolveComponent('NuxtLink') : 'div'" :to="to">
    <UCard :class="to ? 'hover:ring-primary-500 transition-all cursor-pointer' : ''">
      <div class="flex items-center gap-4">
        <div
          class="flex items-center justify-center w-12 h-12 rounded-lg"
          :class="`bg-${color}-100 dark:bg-${color}-900/20`"
        >
          <UIcon
            :name="icon"
            class="w-6 h-6"
            :class="`text-${color}-500`"
          />
        </div>
        <div>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ label }}</p>
          <p class="text-2xl font-bold">{{ value }}</p>
          <p v-if="description" class="text-xs text-gray-400 dark:text-gray-500 mt-0.5">{{ description }}</p>
        </div>
      </div>
    </UCard>
  </component>
</template>
