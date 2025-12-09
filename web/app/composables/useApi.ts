// API composable for making requests to the deployer backend

export function useApiBase() {
  const config = useRuntimeConfig()
  return config.public.apiBase as string
}

export function useApiFetch<T>(path: string, options?: { server?: boolean }) {
  const config = useRuntimeConfig()
  const baseURL = config.public.apiBase as string
  return useFetch<T>(path, {
    baseURL,
    ...options
  } as object)
}

export async function $api<T>(path: string, options?: { method?: string; body?: unknown }): Promise<T> {
  const config = useRuntimeConfig()
  const baseURL = config.public.apiBase as string
  return await $fetch<T>(path, {
    baseURL,
    ...options
  } as object)
}

// Types
export interface App {
  id: string
  name: string
  domain: string
  container_id: string
  image: string
  status: 'pending' | 'building' | 'deploying' | 'running' | 'stopped' | 'failed'
  env: Record<string, string>
  ports: {
    container_port: number
    protocol: string
    expose_port: number
  }
  resources: {
    memory: number
    cpus: number
    replicas: number
  }
  ssl: {
    enabled: boolean
    auto_renew: boolean
  }
  created_at: string
  updated_at: string
}

export interface AppListResponse {
  apps: App[]
  total: number
}

export interface SystemInfo {
  version: string
  status: string
  containers: number
  images: number
}

export interface HealthResponse {
  status: string
  timestamp: string
  podman: string
  podman_error?: string
}
