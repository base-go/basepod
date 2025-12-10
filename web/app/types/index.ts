export interface App {
  id: string
  name: string
  domain?: string
  status: 'running' | 'stopped' | 'pending' | 'error'
  image?: string
  container_id?: string
  ports?: {
    container_port?: number
    host_port?: number
  }
  resources?: {
    memory?: number
    cpus?: number
  }
  ssl?: {
    enabled?: boolean
  }
  env?: Record<string, string>
  created_at: string
  updated_at?: string
}

export interface AppsResponse {
  apps: App[]
  total: number
}

export interface HealthResponse {
  status: string
  podman: 'connected' | 'disconnected'
  podman_error?: string
}

export interface SystemInfoResponse {
  version: string
  containers: number
  images: number
  podman?: {
    version?: string
    socket?: string
  }
}

export interface Template {
  id: string
  name: string
  description: string
  image: string
  image_arm?: string
  port: number
  env: Record<string, string>
  category: string
  icon: string
  arch?: string[]
}

export interface TemplatesResponse {
  templates: Template[]
  system: {
    arch: string
    os: string
    platform: string
  }
}

export interface AuthStatusResponse {
  authRequired: boolean
  authenticated: boolean
}
