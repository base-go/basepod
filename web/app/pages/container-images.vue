<script setup lang="ts">
definePageMeta({
  title: 'Container Images'
})

interface ContainerImage {
  Id: string
  RepoTags: string[]
  RepoDigests: string[]
  Created: string
  Size: number
}

const { data: images, refresh } = await useApiFetch<ContainerImage[]>('/container-images')

const deleting = ref<string | null>(null)
const confirmDelete = ref<ContainerImage | null>(null)

function formatSize(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

function getImageName(image: ContainerImage): string {
  if (image.RepoTags && image.RepoTags.length > 0 && image.RepoTags[0] !== '<none>:<none>') {
    return image.RepoTags[0] ?? image.Id.substring(0, 12)
  }
  return image.Id.substring(0, 12)
}

function getShortId(id: string): string {
  return id.replace('sha256:', '').substring(0, 12)
}

async function deleteImage(image: ContainerImage) {
  deleting.value = image.Id
  try {
    await $api(`/container-images/${encodeURIComponent(image.Id)}?force=true`, {
      method: 'DELETE'
    })
    await refresh()
  } catch (e: any) {
    alert('Failed to delete image: ' + (e.message || 'Unknown error'))
  } finally {
    deleting.value = null
    confirmDelete.value = null
  }
}

const totalSize = computed(() => {
  if (!images.value) return 0
  return images.value.reduce((sum, img) => sum + img.Size, 0)
})
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <div>
        <h2 class="text-xl font-semibold">Container Images</h2>
        <p class="text-gray-500 dark:text-gray-400">
          {{ images?.length || 0 }} images using {{ formatSize(totalSize) }}
        </p>
      </div>
      <UButton icon="i-heroicons-arrow-path" variant="outline" @click="refresh()">
        Refresh
      </UButton>
    </div>

    <UCard v-if="images?.length">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 dark:border-gray-700">
              <th class="text-left py-3 px-4 font-medium text-gray-500">Image</th>
              <th class="text-left py-3 px-4 font-medium text-gray-500">ID</th>
              <th class="text-left py-3 px-4 font-medium text-gray-500">Size</th>
              <th class="text-left py-3 px-4 font-medium text-gray-500">Created</th>
              <th class="text-right py-3 px-4 font-medium text-gray-500">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="image in images"
              :key="image.Id"
              class="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50"
            >
              <td class="py-3 px-4">
                <div class="flex items-center gap-2">
                  <UIcon name="i-heroicons-archive-box" class="w-4 h-4 text-gray-400" />
                  <span class="font-medium">{{ getImageName(image) }}</span>
                </div>
                <div v-if="image.RepoTags && image.RepoTags.length > 1" class="mt-1">
                  <UBadge
                    v-for="tag in image.RepoTags.slice(1)"
                    :key="tag"
                    variant="subtle"
                    color="neutral"
                    size="xs"
                    class="mr-1"
                  >
                    {{ tag }}
                  </UBadge>
                </div>
              </td>
              <td class="py-3 px-4">
                <code class="text-xs text-gray-500">{{ getShortId(image.Id) }}</code>
              </td>
              <td class="py-3 px-4">{{ formatSize(image.Size) }}</td>
              <td class="py-3 px-4 text-gray-500">{{ formatDate(image.Created) }}</td>
              <td class="py-3 px-4 text-right">
                <UButton
                  v-if="confirmDelete?.Id !== image.Id"
                  icon="i-heroicons-trash"
                  variant="ghost"
                  color="error"
                  size="xs"
                  @click="confirmDelete = image"
                />
                <div v-else class="flex items-center justify-end gap-1">
                  <UButton
                    size="xs"
                    variant="ghost"
                    @click="confirmDelete = null"
                  >
                    Cancel
                  </UButton>
                  <UButton
                    size="xs"
                    color="error"
                    :loading="deleting === image.Id"
                    @click="deleteImage(image)"
                  >
                    Delete
                  </UButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </UCard>

    <div v-else class="text-center py-16 text-gray-500">
      <UIcon name="i-heroicons-archive-box" class="w-12 h-12 mx-auto mb-2 opacity-50" />
      <p>No container images found</p>
      <p class="text-sm mt-1">Podman may not be connected</p>
    </div>
  </div>
</template>
