<script setup lang="ts">
definePageMeta({
  title: 'API Documentation'
})

interface Param {
  name: string
  type: string
  required: boolean
  description: string
}

interface Endpoint {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  description: string
  auth: boolean
  params?: Param[]
  body?: Param[]
  queryParams?: Param[]
}

interface EndpointGroup {
  label: string
  endpoints: Endpoint[]
}

const groups: EndpointGroup[] = [
  {
    label: 'AUTHENTICATION',
    endpoints: [
      {
        method: 'POST', path: '/api/auth/login', description: 'Authenticate and get a session token', auth: false,
        body: [
          { name: 'password', type: 'string', required: true, description: 'Account password' },
          { name: 'email', type: 'string', required: false, description: 'User email (optional for admin)' }
        ]
      },
      { method: 'POST', path: '/api/auth/logout', description: 'Invalidate the current session', auth: true },
      { method: 'GET', path: '/api/auth/status', description: 'Check authentication status', auth: false },
      {
        method: 'POST', path: '/api/auth/setup', description: 'Initial admin password setup (first run only)', auth: false,
        body: [
          { name: 'password', type: 'string', required: true, description: 'Admin password (min 8 characters)' }
        ]
      },
      {
        method: 'POST', path: '/api/auth/change-password', description: 'Change current user password', auth: true,
        body: [
          { name: 'current_password', type: 'string', required: true, description: 'Current password' },
          { name: 'new_password', type: 'string', required: true, description: 'New password (min 8 characters)' }
        ]
      },
      {
        method: 'POST', path: '/api/auth/accept-invite', description: 'Accept an invite and set password', auth: false,
        body: [
          { name: 'invite_token', type: 'string', required: true, description: 'Invitation token from invite link' },
          { name: 'password', type: 'string', required: true, description: 'New password (min 8 characters)' }
        ]
      }
    ]
  },
  {
    label: 'USERS',
    endpoints: [
      { method: 'GET', path: '/api/users', description: 'List all users (admin only)', auth: true },
      {
        method: 'POST', path: '/api/users/invite', description: 'Invite a new user (admin only)', auth: true,
        body: [
          { name: 'email', type: 'string', required: true, description: 'User email address' },
          { name: 'role', type: 'string', required: false, description: 'Role: "admin", "deployer", or "viewer" (default: "viewer")' }
        ]
      },
      {
        method: 'PUT', path: '/api/users/{id}/role', description: 'Change a user role (admin only)', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'User ID' }],
        body: [{ name: 'role', type: 'string', required: true, description: 'New role: "admin", "deployer", or "viewer"' }]
      },
      {
        method: 'DELETE', path: '/api/users/{id}', description: 'Delete a user (admin only)', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'User ID' }]
      }
    ]
  },
  {
    label: 'APPS',
    endpoints: [
      { method: 'GET', path: '/api/apps', description: 'List all applications', auth: true },
      {
        method: 'POST', path: '/api/apps', description: 'Create a new application', auth: true,
        body: [
          { name: 'name', type: 'string', required: true, description: 'Application name' },
          { name: 'type', type: 'string', required: false, description: '"container" (default) or "mlx"' },
          { name: 'domain', type: 'string', required: false, description: 'Domain name (auto-generated if empty)' },
          { name: 'image', type: 'string', required: false, description: 'Docker image for image-based deployments' },
          { name: 'env', type: 'object', required: false, description: 'Environment variables key-value pairs' },
          { name: 'port', type: 'int', required: false, description: 'Container port (default: 8080)' },
          { name: 'memory', type: 'int', required: false, description: 'Memory limit in bytes' },
          { name: 'cpus', type: 'float', required: false, description: 'CPU limit' },
          { name: 'enable_ssl', type: 'bool', required: false, description: 'Enable SSL' },
          { name: 'volumes', type: 'array', required: false, description: 'Volume mounts [{name, path}]' }
        ]
      },
      {
        method: 'GET', path: '/api/apps/{id}', description: 'Get application details', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'PUT', path: '/api/apps/{id}', description: 'Update application settings', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }],
        body: [
          { name: 'name', type: 'string', required: false, description: 'New app name' },
          { name: 'domain', type: 'string', required: false, description: 'New domain' },
          { name: 'aliases', type: 'array', required: false, description: 'Additional domain aliases' },
          { name: 'image', type: 'string', required: false, description: 'New image' },
          { name: 'env', type: 'object', required: false, description: 'Environment variables' },
          { name: 'port', type: 'int', required: false, description: 'Container port' },
          { name: 'memory', type: 'int', required: false, description: 'Memory limit in bytes' },
          { name: 'cpus', type: 'float', required: false, description: 'CPU limit' },
          { name: 'enable_ssl', type: 'bool', required: false, description: 'Enable SSL' },
          { name: 'expose_external', type: 'bool', required: false, description: 'Expose externally' },
          { name: 'volumes', type: 'array', required: false, description: 'Volume mounts' },
          { name: 'health_check', type: 'object', required: false, description: 'Health check configuration' },
          { name: 'deployment', type: 'object', required: false, description: 'Deployment configuration' }
        ]
      },
      {
        method: 'DELETE', path: '/api/apps/{id}', description: 'Delete an application and its container', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  },
  {
    label: 'APP LIFECYCLE',
    endpoints: [
      {
        method: 'POST', path: '/api/apps/{id}/start', description: 'Start a stopped application', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/stop', description: 'Stop a running application', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/restart', description: 'Restart an application', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/deploy', description: 'Deploy an application from its configured image', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/rollback', description: 'Rollback to a previous deployment', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }],
        body: [{ name: 'deployment_id', type: 'string', required: true, description: 'Deployment ID to rollback to' }]
      },
      {
        method: 'GET', path: '/api/apps/{id}/logs', description: 'Get container logs', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }],
        queryParams: [{ name: 'tail', type: 'string', required: false, description: 'Number of log lines (default: "100")' }]
      },
      {
        method: 'GET', path: '/api/apps/{id}/terminal', description: 'WebSocket terminal connection', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  },
  {
    label: 'HEALTH & METRICS',
    endpoints: [
      {
        method: 'GET', path: '/api/apps/{id}/health', description: 'Get app health status', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/health/check', description: 'Trigger a health check now', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'GET', path: '/api/apps/{id}/metrics', description: 'Get resource usage metrics', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }],
        queryParams: [{ name: 'period', type: 'string', required: false, description: 'Time period: "1h" (default), "24h", "7d"' }]
      },
      {
        method: 'GET', path: '/api/apps/{id}/access-logs', description: 'Get Caddy access logs for an app', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  },
  {
    label: 'DEPLOYMENTS',
    endpoints: [
      {
        method: 'POST', path: '/api/deploy', description: 'Deploy from source code (multipart upload)', auth: true,
        body: [
          { name: 'config', type: 'json', required: true, description: 'JSON-encoded deploy config (name, type, domain, port, env, volumes)' },
          { name: 'source', type: 'file', required: true, description: 'Source code tarball' }
        ]
      },
      {
        method: 'GET', path: '/api/apps/{id}/deployments/{deployId}/logs', description: 'Get logs for a specific deployment', auth: true,
        params: [
          { name: 'id', type: 'string', required: true, description: 'App ID or name' },
          { name: 'deployId', type: 'string', required: true, description: 'Deployment ID' }
        ]
      }
    ]
  },
  {
    label: 'WEBHOOKS',
    endpoints: [
      {
        method: 'POST', path: '/api/apps/{id}/webhook/setup', description: 'Enable GitHub webhook for auto-deploy', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }],
        body: [{ name: 'git_url', type: 'string', required: true, description: 'Git repository URL' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/webhook', description: 'GitHub webhook receiver (HMAC validated)', auth: false,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID' }]
      },
      {
        method: 'GET', path: '/api/apps/{id}/webhook/deliveries', description: 'List recent webhook deliveries', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  },
  {
    label: 'CRON JOBS',
    endpoints: [
      {
        method: 'GET', path: '/api/apps/{id}/cron', description: 'List cron jobs for an app', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      },
      {
        method: 'POST', path: '/api/apps/{id}/cron', description: 'Create a cron job', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }],
        body: [
          { name: 'name', type: 'string', required: true, description: 'Job name' },
          { name: 'schedule', type: 'string', required: true, description: 'Cron expression (e.g., "0 2 * * *")' },
          { name: 'command', type: 'string', required: true, description: 'Shell command to execute' },
          { name: 'enabled', type: 'bool', required: false, description: 'Enable the job (default: true)' }
        ]
      },
      {
        method: 'PUT', path: '/api/apps/{id}/cron/{jobId}', description: 'Update a cron job', auth: true,
        params: [
          { name: 'id', type: 'string', required: true, description: 'App ID or name' },
          { name: 'jobId', type: 'string', required: true, description: 'Cron job ID' }
        ]
      },
      {
        method: 'DELETE', path: '/api/apps/{id}/cron/{jobId}', description: 'Delete a cron job', auth: true,
        params: [
          { name: 'id', type: 'string', required: true, description: 'App ID or name' },
          { name: 'jobId', type: 'string', required: true, description: 'Cron job ID' }
        ]
      },
      {
        method: 'POST', path: '/api/apps/{id}/cron/{jobId}/run', description: 'Manually trigger a cron job', auth: true,
        params: [
          { name: 'id', type: 'string', required: true, description: 'App ID or name' },
          { name: 'jobId', type: 'string', required: true, description: 'Cron job ID' }
        ]
      },
      {
        method: 'GET', path: '/api/apps/{id}/cron/{jobId}/executions', description: 'List cron job execution history', auth: true,
        params: [
          { name: 'id', type: 'string', required: true, description: 'App ID or name' },
          { name: 'jobId', type: 'string', required: true, description: 'Cron job ID' }
        ]
      }
    ]
  },
  {
    label: 'TEMPLATES',
    endpoints: [
      { method: 'GET', path: '/api/templates', description: 'List all available one-click app templates', auth: true },
      {
        method: 'POST', path: '/api/templates/{id}/deploy', description: 'Deploy a template as a new app', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Template ID' }],
        body: [
          { name: 'name', type: 'string', required: false, description: 'App name (auto-generated if empty)' },
          { name: 'domain', type: 'string', required: false, description: 'Domain name (auto-generated if empty)' },
          { name: 'env', type: 'object', required: false, description: 'Environment variables (merged with template defaults)' },
          { name: 'enableSSL', type: 'bool', required: false, description: 'Enable SSL' },
          { name: 'exposeExternal', type: 'bool', required: false, description: 'Expose externally' }
        ]
      }
    ]
  },
  {
    label: 'MLX / LOCAL LLM',
    endpoints: [
      { method: 'GET', path: '/api/mlx/status', description: 'Get MLX server status and active model', auth: true },
      { method: 'GET', path: '/api/mlx/models', description: 'List available MLX models', auth: true },
      {
        method: 'POST', path: '/api/mlx/pull', description: 'Download an MLX model', auth: true,
        body: [{ name: 'model_id', type: 'string', required: true, description: 'HuggingFace model ID (e.g., "mlx-community/Llama-3.2-1B")' }]
      },
      { method: 'GET', path: '/api/mlx/pull/progress', description: 'Get model download progress', auth: true },
      { method: 'POST', path: '/api/mlx/pull/cancel', description: 'Cancel model download', auth: true },
      {
        method: 'POST', path: '/api/mlx/run', description: 'Start the MLX server with a model', auth: true,
        body: [{ name: 'model_id', type: 'string', required: true, description: 'Model ID to load' }]
      },
      { method: 'POST', path: '/api/mlx/stop', description: 'Stop the MLX server', auth: true },
      {
        method: 'DELETE', path: '/api/mlx/models/{id}', description: 'Delete a downloaded model', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Model ID' }]
      }
    ]
  },
  {
    label: 'CHAT',
    endpoints: [
      {
        method: 'GET', path: '/api/chat/messages/{modelId}', description: 'Get chat history for a model', auth: true,
        params: [{ name: 'modelId', type: 'string', required: true, description: 'Model ID' }]
      },
      {
        method: 'POST', path: '/api/chat/messages/{modelId}', description: 'Save a chat message', auth: true,
        params: [{ name: 'modelId', type: 'string', required: true, description: 'Model ID' }]
      },
      {
        method: 'DELETE', path: '/api/chat/messages/{modelId}', description: 'Clear chat history for a model', auth: true,
        params: [{ name: 'modelId', type: 'string', required: true, description: 'Model ID' }]
      }
    ]
  },
  {
    label: 'SYSTEM',
    endpoints: [
      { method: 'GET', path: '/api/health', description: 'System health check', auth: false },
      { method: 'GET', path: '/api/system/info', description: 'Get system information (hostname, OS, arch)', auth: true },
      { method: 'GET', path: '/api/system/processes', description: 'List running processes', auth: true },
      { method: 'GET', path: '/api/system/version', description: 'Get version and update availability', auth: true },
      { method: 'POST', path: '/api/system/update', description: 'Trigger system self-update', auth: true },
      { method: 'POST', path: '/api/system/prune', description: 'Prune unused container images', auth: true },
      {
        method: 'POST', path: '/api/system/restart/{service}', description: 'Restart a system service', auth: true,
        params: [{ name: 'service', type: 'string', required: true, description: 'Service name: "caddy" or "basepod"' }]
      },
      { method: 'GET', path: '/api/system/config', description: 'Get system configuration', auth: false },
      {
        method: 'PUT', path: '/api/system/config', description: 'Update system configuration', auth: true,
        body: [
          { name: 'domain', type: 'string', required: false, description: 'Base domain' },
          { name: 'wildcard', type: 'bool', required: false, description: 'Enable wildcard subdomains' }
        ]
      }
    ]
  },
  {
    label: 'STORAGE',
    endpoints: [
      { method: 'GET', path: '/api/system/storage', description: 'Get storage overview (disk usage by category)', auth: true },
      { method: 'GET', path: '/api/system/volumes', description: 'List container volumes with sizes', auth: true },
      {
        method: 'DELETE', path: '/api/system/storage/{id}', description: 'Clear a storage category (e.g., logs, cache)', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Category ID (e.g., "logs", "huggingface")' }]
      },
      { method: 'GET', path: '/api/system/storage/llm', description: 'List LLM model files on disk', auth: true },
      {
        method: 'DELETE', path: '/api/system/storage/llm/{name}', description: 'Delete an LLM model directory', auth: true,
        params: [{ name: 'name', type: 'string', required: true, description: 'Model directory name' }]
      },
      { method: 'GET', path: '/api/container-images', description: 'List all container images', auth: true },
      {
        method: 'DELETE', path: '/api/container-images/{id}', description: 'Delete a container image', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Image ID' }],
        queryParams: [{ name: 'force', type: 'bool', required: false, description: 'Force delete (default: false)' }]
      },
      {
        method: 'GET', path: '/api/images/tags', description: 'Search Docker Hub for image tags', auth: true,
        queryParams: [
          { name: 'image', type: 'string', required: true, description: 'Image name (e.g., "nginx")' },
          { name: 'search', type: 'string', required: false, description: 'Filter tags by keyword' }
        ]
      }
    ]
  },
  {
    label: 'ACTIVITY',
    endpoints: [
      {
        method: 'GET', path: '/api/activity', description: 'List global activity log', auth: true,
        queryParams: [
          { name: 'action', type: 'string', required: false, description: 'Filter by action type (e.g., "deploy")' },
          { name: 'target_id', type: 'string', required: false, description: 'Filter by target ID' },
          { name: 'limit', type: 'int', required: false, description: 'Max records (default: 50)' }
        ]
      },
      {
        method: 'GET', path: '/api/apps/{id}/activity', description: 'List activity for a specific app', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  },
  {
    label: 'NOTIFICATIONS',
    endpoints: [
      { method: 'GET', path: '/api/notifications', description: 'List notification hooks', auth: true },
      {
        method: 'POST', path: '/api/notifications', description: 'Create a notification hook', auth: true,
        body: [
          { name: 'name', type: 'string', required: true, description: 'Hook name' },
          { name: 'type', type: 'string', required: true, description: '"webhook", "slack", or "discord"' },
          { name: 'webhook_url', type: 'string', required: false, description: 'Generic webhook URL' },
          { name: 'slack_webhook_url', type: 'string', required: false, description: 'Slack webhook URL' },
          { name: 'discord_webhook_url', type: 'string', required: false, description: 'Discord webhook URL' },
          { name: 'events', type: 'array', required: true, description: 'Event types to listen for' }
        ]
      },
      {
        method: 'PUT', path: '/api/notifications/{id}', description: 'Update a notification hook', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Notification ID' }]
      },
      {
        method: 'DELETE', path: '/api/notifications/{id}', description: 'Delete a notification hook', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Notification ID' }]
      },
      {
        method: 'POST', path: '/api/notifications/{id}/test', description: 'Send a test notification', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Notification ID' }]
      }
    ]
  },
  {
    label: 'DEPLOY TOKENS',
    endpoints: [
      { method: 'GET', path: '/api/deploy-tokens', description: 'List deploy tokens', auth: true },
      {
        method: 'POST', path: '/api/deploy-tokens', description: 'Create a deploy token', auth: true,
        body: [
          { name: 'name', type: 'string', required: true, description: 'Token name/description' },
          { name: 'scopes', type: 'array', required: false, description: 'Permission scopes (default: ["deploy:*"])' }
        ]
      },
      {
        method: 'DELETE', path: '/api/deploy-tokens/{id}', description: 'Revoke a deploy token', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Token ID' }]
      }
    ]
  },
  {
    label: 'BACKUPS',
    endpoints: [
      { method: 'GET', path: '/api/backups', description: 'List all backups', auth: true },
      {
        method: 'POST', path: '/api/backups', description: 'Create a new backup', auth: true,
        body: [
          { name: 'include_volumes', type: 'bool', required: false, description: 'Include volumes (default: true)' },
          { name: 'include_builds', type: 'bool', required: false, description: 'Include builds (default: false)' },
          { name: 'output_dir', type: 'string', required: false, description: 'Custom output directory' }
        ]
      },
      {
        method: 'GET', path: '/api/backups/{id}', description: 'Get backup details', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Backup ID' }]
      },
      {
        method: 'GET', path: '/api/backups/{id}/download', description: 'Download a backup file', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Backup ID' }]
      },
      {
        method: 'POST', path: '/api/backups/{id}/restore', description: 'Restore from a backup', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Backup ID' }]
      },
      {
        method: 'DELETE', path: '/api/backups/{id}', description: 'Delete a backup', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Backup ID' }]
      }
    ]
  },
  {
    label: 'DATABASE LINKING',
    endpoints: [
      {
        method: 'POST', path: '/api/apps/{id}/link/{dbId}', description: 'Link a database to an app', auth: true,
        params: [
          { name: 'id', type: 'string', required: true, description: 'App ID or name' },
          { name: 'dbId', type: 'string', required: true, description: 'Database app ID or name' }
        ]
      },
      {
        method: 'GET', path: '/api/apps/{id}/connection-info', description: 'Get database connection info', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  },
  {
    label: 'OTHER',
    endpoints: [
      { method: 'GET', path: '/api/caddy/check', description: 'Check Caddy reverse proxy status', auth: false },
      { method: 'GET', path: '/api/containers', description: 'List all Podman containers', auth: true },
      {
        method: 'POST', path: '/api/containers/{id}/import', description: 'Import an existing container as an app', auth: true,
        params: [{ name: 'id', type: 'string', required: true, description: 'Container ID' }]
      },
      {
        method: 'GET', path: '/api/badge/{id}', description: 'Get status badge SVG (public)', auth: false,
        params: [{ name: 'id', type: 'string', required: true, description: 'App ID or name' }]
      }
    ]
  }
]

const selectedEndpoint = ref<Endpoint>(groups[0]!.endpoints[0]!)

const methodColors: Record<string, string> = {
  GET: 'bg-green-600',
  POST: 'bg-blue-600',
  PUT: 'bg-amber-600',
  DELETE: 'bg-red-600'
}

const methodBadgeColors: Record<string, string> = {
  GET: 'bg-green-500/20 text-green-400',
  POST: 'bg-blue-500/20 text-blue-400',
  PUT: 'bg-amber-500/20 text-amber-400',
  DELETE: 'bg-red-500/20 text-red-400'
}

function getBaseUrl(): string {
  if (typeof window !== 'undefined') {
    return window.location.origin
  }
  return 'https://bp.example.com'
}

function buildCurlExample(ep: Endpoint): string {
  const base = getBaseUrl()
  let path = ep.path

  // Replace path params with placeholders
  if (ep.params) {
    for (const p of ep.params) {
      path = path.replace(`{${p.name}}`, `<${p.name}>`)
    }
  }

  let cmd = `curl`
  if (ep.method !== 'GET') {
    cmd += ` -X ${ep.method}`
  }

  if (ep.auth) {
    cmd += ` \\\n  -H "Authorization: Bearer <token>"`
  }

  let url = `${base}${path}`

  // Query params
  if (ep.queryParams?.length) {
    const qs = ep.queryParams.map(q => `${q.name}=...`).join('&')
    url += `?${qs}`
  }

  cmd += ` \\\n  "${url}"`

  // Body
  if (ep.body?.length && ep.method !== 'GET') {
    const bodyObj: Record<string, string> = {}
    for (const b of ep.body) {
      if (b.type === 'file') continue
      bodyObj[b.name] = '...'
    }
    if (Object.keys(bodyObj).length > 0) {
      cmd += ` \\\n  -H "Content-Type: application/json" \\\n  -d '${JSON.stringify(bodyObj)}'`
    }
  }

  return cmd
}

// All parameters combined for display
function getAllParams(ep: Endpoint): { section: string; params: Param[] }[] {
  const sections: { section: string; params: Param[] }[] = []
  if (ep.params?.length) sections.push({ section: 'Path Parameters', params: ep.params })
  if (ep.queryParams?.length) sections.push({ section: 'Query Parameters', params: ep.queryParams })
  if (ep.body?.length) sections.push({ section: 'Request Body', params: ep.body })
  return sections
}
</script>

<template>
  <div class="flex h-[calc(100vh-80px)]">
    <!-- Sidebar -->
    <div class="w-72 shrink-0 border-r border-(--ui-border) overflow-y-auto">
      <div class="p-4 border-b border-(--ui-border)">
        <NuxtLink to="/docs" class="flex items-center gap-2 text-sm text-(--ui-text-muted) hover:text-(--ui-text) transition-colors mb-3">
          <UIcon name="i-heroicons-arrow-left" class="w-4 h-4" />
          Back to Docs
        </NuxtLink>
        <h1 class="text-lg font-bold">API Reference</h1>
      </div>

      <nav class="p-2">
        <div v-for="group in groups" :key="group.label" class="mb-4">
          <div class="px-3 py-1.5 text-xs font-semibold text-(--ui-text-dimmed) tracking-wider">
            {{ group.label }}
          </div>
          <button
            v-for="ep in group.endpoints"
            :key="ep.path + ep.method"
            class="w-full flex items-center gap-2 px-3 py-1.5 text-sm rounded-md transition-colors text-left"
            :class="selectedEndpoint === ep
              ? 'bg-primary-500/10 text-primary-500'
              : 'text-(--ui-text-muted) hover:bg-(--ui-bg-muted)'"
            @click="selectedEndpoint = ep"
          >
            <span
              class="text-[10px] font-bold px-1.5 py-0.5 rounded shrink-0 min-w-[38px] text-center"
              :class="methodBadgeColors[ep.method]"
            >
              {{ ep.method }}
            </span>
            <span class="truncate font-mono text-xs">{{ ep.path }}</span>
          </button>
        </div>
      </nav>
    </div>

    <!-- Main Content -->
    <div class="flex-1 overflow-y-auto">
      <div class="max-w-4xl mx-auto p-8">
        <!-- Base URL -->
        <div class="flex items-center justify-end mb-6 text-sm text-(--ui-text-muted)">
          <span class="mr-2">Base URL:</span>
          <code class="px-3 py-1 bg-(--ui-bg-muted) rounded-md font-mono text-xs">{{ getBaseUrl() }}</code>
        </div>

        <!-- Endpoint Header -->
        <div class="flex items-center gap-3 mb-4">
          <span
            class="text-sm font-bold px-3 py-1.5 rounded text-white"
            :class="methodColors[selectedEndpoint.method]"
          >
            {{ selectedEndpoint.method }}
          </span>
          <code class="text-xl font-mono font-semibold">{{ selectedEndpoint.path }}</code>
        </div>

        <p class="text-(--ui-text-muted) mb-6">{{ selectedEndpoint.description }}</p>

        <!-- Auth badge -->
        <div class="mb-6">
          <UBadge v-if="selectedEndpoint.auth" color="warning" variant="soft">
            <UIcon name="i-heroicons-lock-closed" class="w-3 h-3 mr-1" />
            Requires Authentication
          </UBadge>
          <UBadge v-else color="success" variant="soft">
            <UIcon name="i-heroicons-lock-open" class="w-3 h-3 mr-1" />
            Public
          </UBadge>
        </div>

        <!-- Parameters -->
        <div v-for="section in getAllParams(selectedEndpoint)" :key="section.section" class="mb-8">
          <h3 class="text-sm font-semibold uppercase tracking-wider text-(--ui-text-dimmed) mb-3">
            {{ section.section }}
          </h3>
          <div class="border border-(--ui-border) rounded-lg overflow-hidden">
            <table class="w-full text-sm">
              <thead>
                <tr class="border-b border-(--ui-border) bg-(--ui-bg-muted)">
                  <th class="text-left px-4 py-2.5 font-medium text-(--ui-text-dimmed) w-1/4">NAME</th>
                  <th class="text-left px-4 py-2.5 font-medium text-(--ui-text-dimmed) w-1/6">TYPE</th>
                  <th class="text-left px-4 py-2.5 font-medium text-(--ui-text-dimmed)">DESCRIPTION</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="param in section.params" :key="param.name" class="border-b border-(--ui-border) last:border-0">
                  <td class="px-4 py-3">
                    <code class="bg-(--ui-bg-muted) px-2 py-0.5 rounded text-sm font-mono">{{ param.name }}</code>
                  </td>
                  <td class="px-4 py-3">
                    <span class="text-primary-500 text-sm">{{ param.type }}</span>
                  </td>
                  <td class="px-4 py-3 text-(--ui-text-muted)">
                    {{ param.description }}
                    <span v-if="param.required" class="text-red-400 text-xs ml-1">(required)</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- No parameters -->
        <div v-if="getAllParams(selectedEndpoint).length === 0" class="mb-8 text-sm text-(--ui-text-dimmed)">
          No parameters required.
        </div>

        <!-- Example Request -->
        <div class="mb-8">
          <h3 class="text-sm font-semibold uppercase tracking-wider text-(--ui-text-dimmed) mb-3">
            Example Request
          </h3>
          <pre class="bg-gray-950 text-gray-300 p-4 rounded-lg font-mono text-sm overflow-x-auto leading-relaxed">{{ buildCurlExample(selectedEndpoint) }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>
