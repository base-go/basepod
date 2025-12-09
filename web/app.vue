<script setup lang="ts">
const colorMode = useColorMode()

// Set default to dark
onMounted(() => {
  if (!localStorage.getItem('nuxt-color-mode')) {
    colorMode.preference = 'dark'
  }
})
</script>

<template>
  <UApp>
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </UApp>
</template>
