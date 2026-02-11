<script setup lang="ts">
definePageMeta({
  layout: false
})

const route = useRoute()
const inviteToken = computed(() => route.query.invite as string || '')
const isInvite = computed(() => !!inviteToken.value)

const password = ref('')
const confirmPassword = ref('')
const error = ref('')
const loading = ref(false)

const setup = async () => {
  error.value = ''

  if (password.value.length < 8) {
    error.value = 'Password must be at least 8 characters'
    return
  }

  if (password.value !== confirmPassword.value) {
    error.value = 'Passwords do not match'
    return
  }

  loading.value = true

  try {
    if (isInvite.value) {
      // Accept invite — set password for invited user
      await $fetch('/api/auth/accept-invite', {
        method: 'POST',
        body: { invite_token: inviteToken.value, password: password.value }
      })
    } else {
      // Initial admin setup
      await $fetch('/api/auth/setup', {
        method: 'POST',
        body: { password: password.value }
      })
    }
    // Clear nuxt data cache and redirect to dashboard
    clearNuxtData()
    await navigateTo('/', { replace: true, external: true })
  } catch (e: unknown) {
    const err = e as { data?: { error?: string } }
    error.value = err.data?.error || 'Setup failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
    <div class="w-full max-w-sm">
      <div class="bg-(--ui-bg-elevated) rounded-lg shadow-lg p-8">
        <div class="text-center mb-8">
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">
            {{ isInvite ? 'Accept Invite' : 'Welcome to Basepod' }}
          </h1>
          <p class="text-gray-500 dark:text-gray-400 mt-2">
            {{ isInvite ? 'Set your password to join the team' : 'Set up your admin password to get started' }}
          </p>
        </div>

        <form class="space-y-6" @submit.prevent="setup">
          <UFormField label="Password">
            <UInput
              v-model="password"
              type="password"
              placeholder="Enter password (min 8 characters)"
              size="lg"
              autofocus
              :disabled="loading"
            />
          </UFormField>

          <UFormField label="Confirm Password">
            <UInput
              v-model="confirmPassword"
              type="password"
              placeholder="Confirm password"
              size="lg"
              :disabled="loading"
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
            size="lg"
            :loading="loading"
          >
            {{ isInvite ? 'Set Password & Join' : 'Complete Setup' }}
          </UButton>
        </form>
      </div>
    </div>
  </div>
</template>
