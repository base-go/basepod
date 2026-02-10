export interface VolumeMount {
  name: string
  host_path: string
  container_path: string
  read_only?: boolean
}

export type AppType = 'container' | 'mlx' | 'static'

export interface MLXConfig {
  model: string
  max_tokens?: number
  context_size?: number
  temperature?: number
  pid?: number
}

export interface DeploymentRecord {
  id: string
  commit_hash?: string
  commit_msg?: string
  branch?: string
  status: string
  deployed_at: string
}

export interface HealthCheckConfig {
  endpoint: string
  interval: number
  timeout: number
  max_failures: number
  auto_restart: boolean
}

export interface AppHealthStatus {
  status: 'healthy' | 'unhealthy' | 'unknown'
  last_check: string
  last_success: string
  consecutive_failures: number
  last_error?: string
  total_checks: number
  total_failures: number
}

export interface DeploymentConfig {
  source?: string
  dockerfile?: string
  build_context?: string
  branch?: string
  auto_deploy?: boolean
  git_url?: string
  webhook_secret?: string
}

export interface WebhookSetupResponse {
  webhook_url: string
  secret: string
  branch: string
}

export interface WebhookDelivery {
  id: string
  app_id: string
  event: string
  branch?: string
  commit?: string
  message?: string
  status: 'success' | 'failed' | 'skipped' | 'deploying'
  error?: string
  created_at: string
}

export interface App {
  id: string
  name: string
  type?: AppType
  domain?: string
  aliases?: string[]
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
  deployment?: DeploymentConfig
  deployments?: DeploymentRecord[]
  mlx?: MLXConfig
  health_check?: HealthCheckConfig
  health?: AppHealthStatus
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
  category: string
  description?: string
  downloaded: boolean
  downloaded_at?: string
  required_ram_gb?: number
  can_run?: boolean
}

export interface MLXModelsResponse {
  models: MLXModel[]
  supported: boolean
  running: boolean
  port: number
  endpoint: string
  active_model: string
  platform?: string
  unsupported_reason?: string
}

export interface MLXStatusResponse {
  supported: boolean
  platform: string
  running: boolean
  port: number
  pid: number
  active_model: string
  unsupported_reason?: string
}

export interface MLXDownloadProgress {
  model_id: string
  status: 'downloading' | 'completed' | 'error' | 'cancelled' | 'not_found'
  progress: number
  bytes_total: number
  bytes_done: number
  speed: number
  eta: number
  message: string
}

// Cron Jobs
export interface CronJob {
  id: string
  app_id: string
  name: string
  schedule: string
  command: string
  enabled: boolean
  last_run?: string
  last_status?: 'success' | 'failed' | 'running'
  last_error?: string
  next_run?: string
  created_at: string
  updated_at: string
}

export interface CronExecution {
  id: string
  cron_job_id: string
  started_at: string
  ended_at?: string
  status: 'success' | 'failed' | 'running'
  output: string
  exit_code?: number
}

// Activity Log
export interface ActivityLog {
  id: string
  actor_type: 'user' | 'system' | 'webhook'
  action: string
  target_type?: string
  target_id?: string
  target_name?: string
  details?: string
  status?: string
  ip_address?: string
  created_at: string
}

// Notification Hooks
export interface NotificationConfig {
  id: string
  name: string
  type: 'webhook' | 'slack' | 'discord'
  enabled: boolean
  scope: 'global' | 'app'
  scope_id?: string
  webhook_url?: string
  slack_webhook_url?: string
  discord_webhook_url?: string
  events: string[]
  created_at: string
  updated_at: string
}

// Deploy Tokens
export interface DeployToken {
  id: string
  name: string
  prefix: string
  scopes: string[]
  last_used_at?: string
  created_at: string
  expires_at?: string
}

// App Metrics
export interface AppMetric {
  id: number
  app_id: string
  cpu_percent: number
  mem_usage: number
  mem_limit: number
  net_input: number
  net_output: number
  recorded_at: string
}

export interface AppMetricsResponse {
  app_id: string
  period: string
  metrics: AppMetric[]
  current?: {
    cpu_percent: number
    mem_usage: number
    mem_limit: number
    net_input: number
    net_output: number
  }
}

// Users
export interface User {
  id: string
  email: string
  role: 'admin' | 'deployer' | 'viewer'
  created_at: string
  last_login_at?: string
}
