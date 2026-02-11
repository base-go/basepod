<script setup lang="ts">
definePageMeta({
  layout: false
})

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

const login = async () => {
  error.value = ''
  loading.value = true

  try {
    await $fetch('/api/auth/login', {
      method: 'POST',
      body: { email: email.value, password: password.value }
    })
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
            Sign in to your account
          </p>
        </div>

        <form class="space-y-6" @submit.prevent="login">
          <UFormField label="Email">
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
            Sign In
          </UButton>
        </form>

        <p class="mt-6 text-center text-sm text-gray-500 dark:text-gray-400">
          Admin?
          <NuxtLink to="/login" class="text-primary-500 hover:text-primary-600 font-medium">
            Sign in here
          </NuxtLink>
        </p>
      </div>
    </div>
  </div>
</template>
