// API composable for making requests to the deployer backend

export function useApi() {
  const config = useRuntimeConfig()

  const baseURL = config.public.apiBase

  async function get<T>(path: string): Promise<T> {
    return await $fetch<T>(path, { baseURL })
  }

  async function post<T>(path: string, body?: any): Promise<T> {
    return await $fetch<T>(path, {
      method: 'POST',
      baseURL,
      body
    })
  }

  async function put<T>(path: string, body?: any): Promise<T> {
    return await $fetch<T>(path, {
      method: 'PUT',
      baseURL,
      body
    })
  }

  async function del<T>(path: string): Promise<T> {
    return await $fetch<T>(path, {
      method: 'DELETE',
      baseURL
    })
  }

  return {
    get,
    post,
    put,
    del,
    baseURL
  }
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
