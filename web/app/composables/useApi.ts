// API composable for making requests to the basepod backend

export function useApiBase() {
  const config = useRuntimeConfig()
  return config.public.apiBase as string
}

// SSR-safe fetch with auth credentials
export function useApiFetch<T>(path: string, options?: { server?: boolean }) {
  const config = useRuntimeConfig()
  const baseURL = config.public.apiBase as string
  return useFetch<T>(path, {
    baseURL,
    credentials: 'include',
    ...options
  } as object)
}

// Client-side fetch with auth credentials
export async function $api<T>(path: string, options?: { method?: string; body?: unknown }): Promise<T> {
  const config = useRuntimeConfig()
  const baseURL = config.public.apiBase as string
  return await $fetch<T>(path, {
    baseURL,
    credentials: 'include',
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

export interface DomainConfig {
  root: string
  suffix: string
  wildcard: boolean
}

export interface ConfigResponse {
  domain: DomainConfig
}

// Helper to generate app domain from config
export function getAppDomain(appName: string, domainConfig: DomainConfig): string {
  if (domainConfig.root) {
    return `${appName}.${domainConfig.root}`
  }
  const suffix = domainConfig.suffix || '.pod'
  return `${appName}${suffix}`
}
