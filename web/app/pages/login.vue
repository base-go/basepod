<script setup lang="ts">
definePageMeta({
  layout: false
})

const password = ref('')
const error = ref('')
const loading = ref(false)

const login = async () => {
  error.value = ''
  loading.value = true

  try {
    await $fetch('/api/auth/login', {
      method: 'POST',
      body: { password: password.value }
    })
    // Clear nuxt data cache and force reload to pick up new auth state
    clearNuxtData()
    await navigateTo('/', { replace: true, external: true })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    error.value = err.data?.error || 'Invalid password'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
    <div class="w-full max-w-md px-4">
      <div class="bg-white dark:bg-gray-800 rounded-xl shadow-xl p-10">
        <div class="text-center mb-10">
          <BasepodLogo status="ok" class="mx-auto mb-4" />
          <p class="text-gray-500 dark:text-gray-400">Enter your password to continue</p>
        </div>

        <form class="space-y-6" @submit.prevent="login">
          <UFormField label="Password">
            <UInput
              v-model="password"
              type="password"
              placeholder="Enter password"
              size="xl"
              autofocus
              :disabled="loading"
              class="w-full"
              :ui="{ base: 'w-full' }"
            />
          </UFormField>

          <UAlert
            v-if="error"
            color="error"
            variant="soft"
            :title="error"
          />

          <UButton
            type="submit"
            block
            size="xl"
            :loading="loading"
          >
            Login
          </UButton>
        </form>
      </div>
    </div>
  </div>
</template>
