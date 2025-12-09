<script setup lang="ts">
import type { TemplatesResponse, Template } from '~/types'

definePageMeta({
  title: 'Templates'
})

const toast = useToast()
const { data } = await useApiFetch<TemplatesResponse>('/templates')

const isDeployModalOpen = ref(false)
const selectedTemplate = ref<Template | null>(null)
const deployForm = ref({
  name: '',
  enableSSL: false
})

// Auto-generate domain from app name
const autoDomain = computed(() => {
  if (!deployForm.value.name) return ''
  return `${deployForm.value.name.toLowerCase().replace(/[^a-z0-9-]/g, '-')}.pod`
})

const categories = computed(() => {
  if (!data.value?.templates) return []
  const cats = new Set(data.value.templates.map(t => t.category))
  return Array.from(cats)
})

const templatesByCategory = computed(() => {
  if (!data.value?.templates) return {} as Record<string, Template[]>
  const result: Record<string, Template[]> = {}
  for (const template of data.value.templates) {
    const cat = template.category
    if (!result[cat]) {
      result[cat] = []
    }
    result[cat]!.push(template)
  }
  return result
})

const categoryLabels: Record<string, string> = {
  database: 'Databases',
  admin: 'Admin Tools',
  webserver: 'Web Servers',
  cms: 'CMS / Apps',
  devtools: 'Dev Tools',
  communication: 'Communication',
  automation: 'Automation',
  framework: 'Frameworks'
}

function openDeployModal(template: Template) {
  selectedTemplate.value = template
  deployForm.value = {
    name: template.id,
    enableSSL: false
  }
  isDeployModalOpen.value = true
}

async function deployTemplate() {
  if (!selectedTemplate.value) return

  try {
    await $api(`/templates/${selectedTemplate.value.id}/deploy`, {
      method: 'POST',
      body: deployForm.value
    })
    isDeployModalOpen.value = false
    toast.add({ title: `${selectedTemplate.value.name} deployed`, color: 'success' })
    navigateTo('/apps')
  } catch (error) {
    const message = error && typeof error === 'object' && 'data' in error
      ? (error as { data?: { error?: string } }).data?.error
      : 'An unexpected error occurred'
    toast.add({ title: 'Failed to deploy', description: message, color: 'error' })
  }
}
</script>

<template>
  <div>
    <!-- Header -->
    <div class="mb-6">
      <h2 class="text-xl font-semibold">One-Click Apps</h2>
      <p class="text-gray-500 dark:text-gray-400">
        Deploy popular apps with a single click
        <span v-if="data?.system" class="text-sm ml-2">({{ data.system.platform }})</span>
      </p>
    </div>

    <!-- Templates by Category -->
    <div class="space-y-8">
      <div v-for="category in categories" :key="category">
        <h3 class="text-lg font-medium mb-4">{{ categoryLabels[category] || category }}</h3>
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          <UCard
            v-for="template in templatesByCategory[category]"
            :key="template.id"
            class="hover:ring-2 hover:ring-primary-500 cursor-pointer transition-all"
            @click="openDeployModal(template)"
          >
            <div class="flex items-start gap-4">
              <div class="flex-shrink-0 w-12 h-12 bg-primary-100 dark:bg-primary-900 rounded-lg flex items-center justify-center">
                <UIcon :name="template.icon" class="w-6 h-6 text-primary-500" />
              </div>
              <div class="flex-1 min-w-0">
                <h4 class="font-medium">{{ template.name }}</h4>
                <p class="text-sm text-gray-500 dark:text-gray-400 line-clamp-2">
                  {{ template.description }}
                </p>
                <div class="mt-2 flex items-center gap-2">
                  <code class="text-xs bg-gray-100 dark:bg-gray-800 px-2 py-0.5 rounded">
                    {{ template.image }}
                  </code>
                  <span class="text-xs text-gray-400">:{{ template.port }}</span>
                </div>
              </div>
            </div>
          </UCard>
        </div>
      </div>
    </div>

    <!-- Empty State -->
    <div v-if="!data?.templates?.length" class="text-center py-12">
      <UIcon name="i-heroicons-cube-transparent" class="w-16 h-16 mx-auto mb-4 text-gray-300" />
      <h3 class="text-lg font-medium mb-2">No templates available</h3>
      <p class="text-gray-500">Check back later for one-click app templates</p>
    </div>

    <!-- Deploy Modal -->
    <UModal v-model:open="isDeployModalOpen" :title="`Deploy ${selectedTemplate?.name}`">
      <template #body>
        <div class="space-y-4">
          <div v-if="selectedTemplate" class="flex items-center gap-3 p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
            <UIcon :name="selectedTemplate.icon" class="w-8 h-8 text-primary-500" />
            <div>
              <div class="font-medium">{{ selectedTemplate.name }}</div>
              <div class="text-sm text-gray-500">{{ selectedTemplate.image }}</div>
            </div>
          </div>

          <UFormField label="App Name">
            <UInput v-model="deployForm.name" placeholder="my-app" />
          </UFormField>

          <UFormField label="Domain">
            <div class="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-800 rounded-md font-mono text-sm">
              <span class="text-gray-500">http://</span>
              <span>{{ autoDomain || 'app-name.pod' }}</span>
            </div>
            <p class="text-xs text-gray-500 mt-1">Auto-assigned based on app name</p>
          </UFormField>

          <!-- Show default environment variables -->
          <div v-if="selectedTemplate && Object.keys(selectedTemplate.env).length">
            <div class="text-sm font-medium mb-2">Default Environment Variables</div>
            <div class="space-y-1">
              <div
                v-for="(value, key) in selectedTemplate.env"
                :key="key"
                class="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded font-mono"
              >
                {{ key }}={{ key === 'url' && autoDomain ? `http://${autoDomain}` : value }}
              </div>
            </div>
            <p class="text-xs text-gray-500 mt-2">
              You can modify these in the app settings after deployment
            </p>
          </div>
        </div>
      </template>

      <template #footer>
        <div class="flex justify-end gap-2">
          <UButton variant="ghost" @click="isDeployModalOpen = false">Cancel</UButton>
          <UButton @click="deployTemplate">Deploy</UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
