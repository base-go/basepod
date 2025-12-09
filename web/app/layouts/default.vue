<script setup lang="ts">
const colorMode = useColorMode()

const navigation = [
  { label: 'Dashboard', icon: 'i-heroicons-home', to: '/' },
  { label: 'Apps', icon: 'i-heroicons-cube', to: '/apps' },
  { label: 'Settings', icon: 'i-heroicons-cog-6-tooth', to: '/settings' }
]
</script>

<template>
  <div class="min-h-screen bg-gray-50 dark:bg-gray-950">
    <!-- Sidebar -->
    <aside class="fixed inset-y-0 left-0 z-50 w-64 bg-white dark:bg-gray-900 border-r border-gray-200 dark:border-gray-800">
      <!-- Logo -->
      <div class="flex items-center gap-2 h-16 px-4 border-b border-gray-200 dark:border-gray-800">
        <UIcon name="i-heroicons-rocket-launch" class="w-8 h-8 text-primary-500" />
        <span class="text-xl font-bold">Deployer</span>
      </div>

      <!-- Navigation -->
      <nav class="p-4 space-y-1">
        <UButton
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          :icon="item.icon"
          variant="ghost"
          color="neutral"
          class="w-full justify-start"
          :class="{ 'bg-gray-100 dark:bg-gray-800': $route.path === item.to }"
        >
          {{ item.label }}
        </UButton>
      </nav>

      <!-- Footer -->
      <div class="absolute bottom-0 left-0 right-0 p-4 border-t border-gray-200 dark:border-gray-800">
        <div class="flex items-center justify-between">
          <span class="text-sm text-gray-500">v0.1.0</span>
          <UButton
            :icon="colorMode.value === 'dark' ? 'i-heroicons-sun' : 'i-heroicons-moon'"
            variant="ghost"
            color="neutral"
            size="sm"
            @click="colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'"
          />
        </div>
      </div>
    </aside>

    <!-- Main content -->
    <main class="pl-64">
      <!-- Header -->
      <header class="sticky top-0 z-40 h-16 bg-white/80 dark:bg-gray-900/80 backdrop-blur border-b border-gray-200 dark:border-gray-800">
        <div class="flex items-center justify-between h-full px-6">
          <h1 class="text-lg font-semibold">
            <slot name="header" />
          </h1>
          <div class="flex items-center gap-4">
            <UButton icon="i-heroicons-bell" variant="ghost" color="neutral" />
            <UAvatar text="U" size="sm" />
          </div>
        </div>
      </header>

      <!-- Page content -->
      <div class="p-6">
        <slot />
      </div>
    </main>
  </div>
</template>
