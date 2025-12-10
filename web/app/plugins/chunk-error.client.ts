// Handle chunk loading errors by reloading the page
// This happens when the app is updated and old cached chunks are no longer available

export default defineNuxtPlugin(() => {
  const router = useRouter()

  router.onError((error) => {
    if (
      error.message.includes('Failed to fetch dynamically imported module') ||
      error.message.includes('Importing a module script failed') ||
      error.message.includes('Loading chunk') ||
      error.message.includes('ChunkLoadError')
    ) {
      // Reload the page to get fresh chunks
      window.location.reload()
    }
  })

  // Also handle unhandled promise rejections for dynamic imports
  if (import.meta.client) {
    window.addEventListener('unhandledrejection', (event) => {
      const message = event.reason?.message || ''
      if (
        message.includes('Failed to fetch dynamically imported module') ||
        message.includes('Importing a module script failed') ||
        message.includes('Loading chunk')
      ) {
        event.preventDefault()
        window.location.reload()
      }
    })
  }
})
