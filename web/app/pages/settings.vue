<script setup lang="ts">
import type { HealthResponse } from '~/types'

definePageMeta({
  title: 'Settings'
})

const { data: health } = await useApiFetch<HealthResponse>('/health')

const settings = ref({
  domain: '',
  email: '',
  enableWildcard: true
})
</script>

<template>
  <div>
    <div class="max-w-3xl space-y-6">
      <!-- Domain Settings -->
      <UCard>
        <template #header>
          <h3 class="font-semibold">Domain Settings</h3>
        </template>

        <div class="space-y-4">
          <UFormField label="Root Domain" help="The main domain for your deployer instance">
            <UInput v-model="settings.domain" placeholder="deployer.example.com" />
          </UFormField>

          <UFormField label="Email" help="Used for Let's Encrypt SSL certificates">
            <UInput v-model="settings.email" type="email" placeholder="admin@example.com" />
          </UFormField>

          <UFormField>
            <UCheckbox v-model="settings.enableWildcard" label="Enable wildcard subdomains" />
          </UFormField>

          <UButton>Save Domain Settings</UButton>
        </div>
      </UCard>

      <!-- System Status -->
      <UCard>
        <template #header>
          <h3 class="font-semibold">System Status</h3>
        </template>

        <div class="space-y-4">
          <div class="flex items-center justify-between py-2 border-b border-gray-200 dark:border-gray-800">
            <div>
              <p class="font-medium">Podman</p>
              <p class="text-sm text-gray-500">Container runtime</p>
            </div>
            <div class="text-right">
              <UBadge :color="health?.podman === 'connected' ? 'success' : 'error'">
                {{ health?.podman === 'connected' ? 'Connected' : 'Disconnected' }}
              </UBadge>
              <p v-if="health?.podman !== 'connected'" class="text-xs text-red-500 mt-1">
                {{ health?.podman_error }}
              </p>
            </div>
          </div>

          <div class="flex items-center justify-between py-2 border-b border-gray-200 dark:border-gray-800">
            <div>
              <p class="font-medium">Caddy</p>
              <p class="text-sm text-gray-500">Reverse proxy</p>
            </div>
            <UBadge color="success">Running</UBadge>
          </div>

          <div class="flex items-center justify-between py-2">
            <div>
              <p class="font-medium">Database</p>
              <p class="text-sm text-gray-500">SQLite</p>
            </div>
            <UBadge color="success">Connected</UBadge>
          </div>
        </div>
      </UCard>

      <!-- Danger Zone -->
      <UCard class="border-red-200 dark:border-red-900">
        <template #header>
          <h3 class="font-semibold text-red-600">Danger Zone</h3>
        </template>

        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <div>
              <p class="font-medium">Reset Configuration</p>
              <p class="text-sm text-gray-500">Reset all settings to defaults</p>
            </div>
            <UButton color="error" variant="soft">Reset</UButton>
          </div>

          <div class="flex items-center justify-between">
            <div>
              <p class="font-medium">Prune Unused Resources</p>
              <p class="text-sm text-gray-500">Remove unused containers, images, and volumes</p>
            </div>
            <UButton color="error" variant="soft">Prune</UButton>
          </div>
        </div>
      </UCard>
    </div>
  </div>
</template>
