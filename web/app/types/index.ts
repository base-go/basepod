export interface VolumeMount {
  name: string
  host_path: string
  container_path: string
  read_only?: boolean
}

export type AppType = 'container' | 'mlx'

export interface MLXConfig {
  model: string
  max_tokens?: number
  context_size?: number
  temperature?: number
  pid?: number
}

export interface App {
  id: string
  name: string
  type?: AppType
  domain?: string
  status: 'running' | 'stopped' | 'pending' | 'building' | 'deploying' | 'failed' | 'error'
  image?: string
  container_id?: string
  ports?: {
    container_port?: number
    host_port?: number
    expose_external?: boolean
  }
  resources?: {
    memory?: number
    cpus?: number
  }
  ssl?: {
    enabled?: boolean
  }
  env?: Record<string, string>
  volumes?: VolumeMount[]
  mlx?: MLXConfig
  internal_host?: string
  external_host?: string
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
  versions?: string[]
  default_version?: string
  has_alpine?: boolean
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

export interface ImageTagsResponse {
  image: string
  tags: string[]
}

export interface MLXModel {
  id: string
  name: string
  size: string
  description: string
}

export interface MLXModelsResponse {
  models: MLXModel[]
  supported: boolean
}

export interface MLXStatusResponse {
  supported: boolean
  platform: string
}
