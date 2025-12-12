<script setup lang="ts">
import type { TemplatesResponse, Template, ImageTagsResponse } from '~/types'
import type { ConfigResponse } from '~/composables/useApi'

definePageMeta({
  title: 'Templates'
})

const toast = useToast()
const { data } = await useApiFetch<TemplatesResponse>('/templates')
const { data: configData } = await useApiFetch<ConfigResponse>('/system/config')

const isDeployModalOpen = ref(false)
const selectedTemplate = ref<Template | null>(null)
const deployForm = ref({
  name: '',
  enableSSL: false,
  version: '',
  useAlpine: true, // Default to alpine when available
  exposeExternal: false // Whether to expose port externally (for databases)
})

// Check if template is a database (no HTTP domain needed)
const isDatabase = computed(() => {
  return selectedTemplate.value?.category === 'database'
})
const envVars = ref<Record<string, string>>({})

// Dynamic tags from API
const availableTags = ref<string[]>([])
const loadingTags = ref(false)
const tagSearchQuery = ref('')

// Filtered tags based on search and alpine toggle
const filteredTags = computed(() => {
  let tags = availableTags.value.length > 0 ? availableTags.value : (selectedTemplate.value?.versions || [])

  // Filter by alpine toggle
  if (deployForm.value.useAlpine && selectedTemplate.value?.has_alpine) {
    tags = tags.filter(tag => tag.toLowerCase().includes('alpine'))
  }

  // Filter by search query
  if (tagSearchQuery.value) {
    const query = tagSearchQuery.value.toLowerCase()
    tags = tags.filter(tag => tag.toLowerCase().includes(query))
  }

  return tags
})

// Auto-generate domain from app name using config
const autoDomain = computed(() => {
  if (!deployForm.value.name) return ''
  const name = deployForm.value.name.toLowerCase().replace(/[^a-z0-9-]/g, '-')
  if (configData.value?.domain?.base) {
    return `${name}.${configData.value.domain.base}`
  }
  return `${name}${configData.value?.domain?.suffix || '.local'}`
})

// Domain suffix for placeholder display
const domainPlaceholder = computed(() => {
  if (configData.value?.domain?.base) {
    return `app-name.${configData.value.domain.base}`
  }
  return `app-name${configData.value?.domain?.suffix || '.local'}`
})

// Protocol based on whether we have a real domain
const protocol = computed(() => {
  return configData.value?.domain?.base ? 'https://' : 'http://'
})

// Build the selected image tag based on selected version
const selectedImage = computed(() => {
  if (!selectedTemplate.value) return ''
  const tmpl = selectedTemplate.value
  const version = deployForm.value.version || tmpl.default_version || (tmpl.versions?.[0] ?? '')

  // If no versions defined, return base image
  if (!version) return tmpl.image

  // Just use the selected tag directly - alpine filtering is done in the dropdown
  return `${tmpl.image}:${version}`
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

async function openDeployModal(template: Template) {
  selectedTemplate.value = template
  deployForm.value = {
    name: template.id,
    enableSSL: false,
    version: template.default_version || (template.versions?.[0] ?? ''),
    useAlpine: template.has_alpine ?? false, // Default to alpine if available
    exposeExternal: false // Database external access disabled by default
  }
  // Copy template env vars so they can be edited
  envVars.value = { ...template.env }
  tagSearchQuery.value = ''
  isDeployModalOpen.value = true

  // Fetch tags from API
  loadingTags.value = true
  try {
    const { data: tagsData } = await useApiFetch<ImageTagsResponse>(`/images/tags?image=${template.image}`)
    if (tagsData.value?.tags?.length) {
      availableTags.value = tagsData.value.tags
    } else {
      availableTags.value = template.versions || []
    }
  } catch {
    availableTags.value = template.versions || []
  } finally {
    loadingTags.value = false
  }
}

async function deployTemplate() {
  if (!selectedTemplate.value) return

  try {
    await $api(`/templates/${selectedTemplate.value.id}/deploy`, {
      method: 'POST',
      body: {
        name: deployForm.value.name,
        enableSSL: deployForm.value.enableSSL,
        version: deployForm.value.version,
        useAlpine: deployForm.value.useAlpine,
        exposeExternal: deployForm.value.exposeExternal,
        env: envVars.value
      }
    })
    isDeployModalOpen.value = false
    toast.add({ title: `Created ${selectedTemplate.value.name}`, description: 'Please wait up to 60 seconds for the app to start', color: 'success' })
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
    <UModal v-model:open="isDeployModalOpen" :title="`Create ${selectedTemplate?.name}`">
      <template #body>
        <div class="space-y-4">
          <div v-if="selectedTemplate" class="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg space-y-3">
            <div class="flex items-center gap-3">
              <UIcon :name="selectedTemplate.icon" class="w-8 h-8 text-primary-500" />
              <div class="flex-1">
                <div class="font-medium">{{ selectedTemplate.name }}</div>
                <code class="text-xs text-gray-500">{{ selectedImage }}</code>
              </div>
            </div>
            <!-- Version and Alpine controls -->
            <div class="flex items-center gap-4">
              <!-- Version dropdown with search -->
              <div class="flex items-center gap-2 flex-1">
                <span class="text-xs text-gray-500 shrink-0">Version:</span>
                <USelectMenu
                  v-model="deployForm.version"
                  v-model:search-term="tagSearchQuery"
                  :items="filteredTags"
                  :loading="loadingTags"
                  searchable
                  :search-input-placeholder="loadingTags ? 'Loading tags...' : 'Search tags...'"
                  class="w-40"
                  size="sm"
                />
                <span v-if="availableTags.length > 0" class="text-xs text-gray-400">
                  ({{ availableTags.length }} tags)
                </span>
              </div>
              <!-- Alpine toggle -->
              <div v-if="selectedTemplate.has_alpine" class="flex items-center gap-2">
                <USwitch v-model="deployForm.useAlpine" size="sm" />
                <span class="text-xs text-gray-500">Alpine</span>
              </div>
            </div>
          </div>

          <UFormField label="App Name">
            <UInput v-model="deployForm.name" placeholder="my-app" />
          </UFormField>

          <!-- Domain for web apps -->
          <UFormField v-if="!isDatabase" label="Domain">
            <div class="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-800 rounded-md font-mono text-sm">
              <span class="text-gray-500">{{ protocol }}</span>
              <span>{{ autoDomain || domainPlaceholder }}</span>
            </div>
            <p class="text-xs text-gray-500 mt-1">Auto-assigned based on app name</p>
          </UFormField>

          <!-- Connection info for databases -->
          <div v-if="isDatabase" class="space-y-3">
            <div class="flex items-center justify-between">
              <span class="text-sm font-medium">External Access</span>
              <USwitch v-model="deployForm.exposeExternal" size="sm" />
            </div>
            <div class="p-3 bg-gray-100 dark:bg-gray-800 rounded-md space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-xs text-gray-500">Internal (container-to-container)</span>
                <code class="text-xs">deployer-{{ deployForm.name }}:{{ selectedTemplate?.port }}</code>
              </div>
              <div v-if="deployForm.exposeExternal" class="flex items-center justify-between">
                <span class="text-xs text-gray-500">External (after deploy)</span>
                <code class="text-xs">{{ configData?.domain?.base || 'localhost' }}:PORT</code>
              </div>
            </div>
            <p class="text-xs text-gray-500">
              {{ deployForm.exposeExternal ? 'Port will be assigned after deployment' : 'Only accessible from other containers' }}
            </p>
          </div>

          <!-- Editable environment variables -->
          <div v-if="selectedTemplate && Object.keys(selectedTemplate.env).length">
            <div class="text-sm font-medium mb-2">Environment Variables</div>
            <div class="space-y-2">
              <div
                v-for="(_, key) in selectedTemplate.env"
                :key="key"
                class="flex items-center gap-2"
              >
                <label class="w-40 text-sm font-mono text-gray-600 dark:text-gray-400 shrink-0">{{ key }}</label>
                <UInput
                  v-model="envVars[key]"
                  class="flex-1"
                  :placeholder="selectedTemplate.env[key]"
                />
              </div>
            </div>
            <p class="text-xs text-gray-500 mt-2">
              You can also modify these in the app settings after deployment
            </p>
          </div>
        </div>
      </template>

      <template #footer>
        <div class="flex justify-end gap-2">
          <UButton variant="ghost" @click="isDeployModalOpen = false">Cancel</UButton>
          <UButton @click="deployTemplate">Create</UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
