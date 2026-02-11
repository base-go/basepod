<script setup lang="ts">
definePageMeta({
  layout: false
})

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)
const multiUser = ref(false)

// Check if multi-user mode is enabled
onMounted(async () => {
  try {
    const config = await $fetch<{ multi_user?: boolean }>('/api/system/config')
    multiUser.value = !!config.multi_user
  } catch {
    // Ignore
  }
})

const login = async () => {
  error.value = ''
  loading.value = true

  try {
    const body: { password: string; email?: string } = { password: password.value }
    if (multiUser.value && email.value) {
      body.email = email.value
    }
    await $fetch('/api/auth/login', {
      method: 'POST',
      body
    })
    // Clear nuxt data cache and force reload to pick up new auth state
    clearNuxtData()
    await navigateTo('/', { replace: true, external: true })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    error.value = err.data?.error || 'Invalid credentials'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
    <div class="w-full max-w-md px-4">
      <div class="bg-(--ui-bg-elevated) rounded-xl shadow-xl p-10">
        <div class="text-center mb-10">
          <BasepodLogo status="ok" class="mx-auto mb-4" />
          <p class="text-gray-500 dark:text-gray-400">
            {{ multiUser ? 'Sign in to your account' : 'Enter your password to continue' }}
          </p>
        </div>

        <form class="space-y-6" @submit.prevent="login">
          <UFormField v-if="multiUser" label="Email">
            <UInput
              v-model="email"
              type="email"
              placeholder="you@example.com"
              size="xl"
              autofocus
              :disabled="loading"
              class="w-full"
              :ui="{ base: 'w-full' }"
            />
          </UFormField>

          <UFormField label="Password">
            <UInput
              v-model="password"
              type="password"
              placeholder="Enter password"
              size="xl"
              :autofocus="!multiUser"
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
