<script setup lang="ts">
import type { SystemInfoResponse, HealthResponse } from '~/types'

interface VersionInfo {
  current: string
  latest: string
  updateAvailable: boolean
}

const colorMode = useColorMode()
const { data: systemInfo } = await useApiFetch<SystemInfoResponse>('/system/info')
const { data: health } = await useApiFetch<HealthResponse>('/health')
const { data: version } = await useApiFetch<VersionInfo>('/system/version')

// Compute logo status based on system state
const logoStatus = computed<'ok' | 'update' | 'error'>(() => {
  // Check for errors first (highest priority)
  if (health.value?.podman !== 'connected') {
    return 'error'
  }
  // Check for updates
  if (version.value?.updateAvailable) {
    return 'update'
  }
  // All good
  return 'ok'
})

const navigation = [
  {
    label: 'Dashboard',
    icon: 'i-heroicons-home',
    to: '/'
  },
  {
    label: 'Apps',
    icon: 'i-heroicons-cube',
    to: '/apps'
  },
  {
    label: 'One-Click Apps',
    icon: 'i-heroicons-squares-plus',
    to: '/templates'
  },
  {
    label: 'One-Click LLMs',
    icon: 'i-heroicons-cpu-chip',
    to: '/llms'
  },
  {
    label: 'Chat',
    icon: 'i-heroicons-chat-bubble-left-right',
    to: '/chat'
  }
]

const bottomNavigation = [
  {
    label: 'Processes',
    icon: 'i-heroicons-queue-list',
    to: '/processes'
  },
  {
    label: 'Activity',
    icon: 'i-heroicons-list-bullet',
    to: '/activity'
  },
  {
    label: 'Storage',
    icon: 'i-heroicons-circle-stack',
    to: '/storage'
  },
  {
    label: 'Documentation',
    icon: 'i-heroicons-book-open',
    to: '/docs'
  },
  {
    label: 'Settings',
    icon: 'i-heroicons-cog-6-tooth',
    to: '/settings'
  }
]

const logout = async () => {
  try {
    await $fetch('/api/auth/logout', { method: 'POST' })
  } catch {
    // Ignore errors
  }
  navigateTo('/login')
}
</script>

<template>
  <UDashboardGroup>
    <UDashboardSidebar collapsible resizable>
      <template #header="{ collapsed = false }">
        <div class="flex items-center" :class="collapsed ? 'justify-center' : ''">
          <BasepodLogo :status="logoStatus" :collapsed="collapsed" />
        </div>
      </template>

      <template #default="{ collapsed = false }">
        <div class="flex flex-col h-full">
          <UNavigationMenu
            :items="navigation"
            orientation="vertical"
            :collapsed="collapsed"
            highlight
            color="primary"
          />
          <div class="flex-1" />
          <UNavigationMenu
            :items="bottomNavigation"
            orientation="vertical"
            :collapsed="collapsed"
            highlight
            color="primary"
          />
        </div>
      </template>

      <template #footer="{ collapsed = false }">
        <div class="flex items-center" :class="collapsed ? 'justify-center' : 'justify-between'">
          <span v-if="!collapsed" class="text-sm text-muted">{{ systemInfo?.version || 'v0.0.0' }}</span>
          <ClientOnly>
            <UButton
              :icon="colorMode.value === 'dark' ? 'i-heroicons-sun' : 'i-heroicons-moon'"
              variant="ghost"
              color="neutral"
              size="sm"
              @click="colorMode.preference = colorMode.value === 'dark' ? 'light' : 'dark'"
            />
            <template #fallback>
              <UButton
                icon="i-heroicons-moon"
                variant="ghost"
                color="neutral"
                size="sm"
              />
            </template>
          </ClientOnly>
        </div>
      </template>
    </UDashboardSidebar>

    <UDashboardPanel>
      <template #header>
        <UDashboardNavbar>
          <template #leading>
            <slot name="header" />
          </template>
          <template #trailing>
            <div class="flex items-center gap-2">
              <UButton
                icon="i-heroicons-arrow-right-on-rectangle"
                variant="ghost"
                color="neutral"
                @click="logout"
              />
            </div>
          </template>
        </UDashboardNavbar>
      </template>

      <template #body>
        <slot />
      </template>
    </UDashboardPanel>
  </UDashboardGroup>
</template>
