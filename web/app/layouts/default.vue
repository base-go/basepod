<script setup lang="ts">
import type { SystemInfoResponse, HealthResponse, AuthUser } from '~/types'

interface VersionInfo {
  current: string
  latest: string
  updateAvailable: boolean
}

const colorMode = useColorMode()
const { data: systemInfo } = await useApiFetch<SystemInfoResponse>('/system/info')
const { data: health } = await useApiFetch<HealthResponse>('/health')
const { data: version } = await useApiFetch<VersionInfo>('/system/version')
const { data: currentUser } = await useApiFetch<AuthUser>('/auth/me')

const isAdmin = computed(() => !currentUser.value || currentUser.value.role === 'admin')

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

const filteredBottomNav = computed(() =>
  bottomNavigation.filter(item =>
    item.to !== '/settings' || isAdmin.value
  )
)

const userInitial = computed(() => {
  if (currentUser.value?.email) {
    return currentUser.value.email.charAt(0).toUpperCase()
  }
  return 'A'
})

const roleColor = computed(() => {
  switch (currentUser.value?.role) {
    case 'admin': return 'error' as const
    case 'deployer': return 'warning' as const
    case 'viewer': return 'info' as const
    default: return 'error' as const
  }
})

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
            :items="filteredBottomNav"
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
              <UPopover>
                <template #default>
                  <UButton variant="ghost" color="neutral" class="flex items-center gap-2">
                    <div class="w-7 h-7 rounded-full bg-primary-100 dark:bg-primary-900 flex items-center justify-center">
                      <span class="text-xs font-medium text-primary-600 dark:text-primary-400">
                        {{ userInitial }}
                      </span>
                    </div>
                    <span v-if="currentUser?.email" class="text-sm hidden sm:inline">{{ currentUser.email }}</span>
                    <UBadge :color="roleColor" variant="soft" size="xs" class="hidden sm:inline-flex">
                      {{ currentUser?.role || 'admin' }}
                    </UBadge>
                  </UButton>
                </template>
                <template #content>
                  <div class="p-2 w-56">
                    <div class="px-3 py-2 border-b border-gray-200 dark:border-gray-700 mb-1">
                      <p class="text-sm font-medium truncate">{{ currentUser?.email || 'Admin' }}</p>
                      <UBadge :color="roleColor" variant="soft" size="xs" class="mt-1">
                        {{ currentUser?.role || 'admin' }}
                      </UBadge>
                    </div>
                    <NuxtLink
                      to="/settings?tab=security"
                      class="flex items-center gap-2 px-3 py-2 text-sm rounded hover:bg-(--ui-bg-muted) transition-colors"
                    >
                      <UIcon name="i-heroicons-key" class="w-4 h-4" />
                      Change Password
                    </NuxtLink>
                    <button
                      class="flex items-center gap-2 px-3 py-2 text-sm rounded hover:bg-(--ui-bg-muted) transition-colors w-full text-left text-red-500"
                      @click="logout"
                    >
                      <UIcon name="i-heroicons-arrow-right-on-rectangle" class="w-4 h-4" />
                      Logout
                    </button>
                  </div>
                </template>
              </UPopover>
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
