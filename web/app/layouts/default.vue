<script setup lang="ts">
import type { SystemInfoResponse } from '~/types'

const colorMode = useColorMode()
const { data: systemInfo } = await useApiFetch<SystemInfoResponse>('/system/info')

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
        <div class="flex items-center gap-2" :class="collapsed ? 'justify-center' : ''">
          <UIcon name="i-heroicons-rocket-launch" class="size-8 text-primary-500 shrink-0" />
          <span v-if="!collapsed" class="text-xl font-bold truncate">Deployer</span>
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
