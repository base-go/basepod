import type { AuthStatusResponse } from '~/types'

export default defineNuxtRouteMiddleware(async (to) => {
  // Skip auth check for login page
  if (to.path === '/login') {
    return
  }

  try {
    const { data } = await useFetch<AuthStatusResponse>('/api/auth/status')

    if (data.value?.authRequired && !data.value?.authenticated) {
      return navigateTo('/login')
    }
  } catch {
    // If we can't check auth status, redirect to login for safety
    return navigateTo('/login')
  }
})
