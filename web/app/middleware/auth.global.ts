export default defineNuxtRouteMiddleware(async (to) => {
  // Skip auth check for login and setup pages
  if (to.path === '/login' || to.path === '/setup') {
    return
  }

  try {
    const { data } = await useFetch<{ needsSetup: boolean; authenticated: boolean }>('/api/auth/status')

    // Redirect to setup if no password configured
    if (data.value?.needsSetup) {
      return navigateTo('/setup')
    }

    // Redirect to login if not authenticated
    if (!data.value?.authenticated) {
      return navigateTo('/login')
    }
  } catch {
    // If we can't check auth status, redirect to login for safety
    return navigateTo('/login')
  }
})
