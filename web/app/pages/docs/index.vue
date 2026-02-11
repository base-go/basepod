<script setup lang="ts">
definePageMeta({
  title: 'Documentation'
})

const sections = [
  {
    id: 'getting-started',
    title: 'Getting Started',
    icon: 'i-heroicons-rocket-launch'
  },
  {
    id: 'apps',
    title: 'Managing Apps',
    icon: 'i-heroicons-cube'
  },
  {
    id: 'templates',
    title: 'One-Click Apps',
    icon: 'i-heroicons-squares-plus'
  },
  {
    id: 'databases',
    title: 'Databases',
    icon: 'i-heroicons-circle-stack'
  },
  {
    id: 'domains',
    title: 'Domains & SSL',
    icon: 'i-heroicons-globe-alt'
  },
  {
    id: 'api',
    title: 'API Reference',
    icon: 'i-heroicons-code-bracket'
  }
]

const activeSection = ref('getting-started')
</script>

<template>
  <div>
    <div class="mb-6">
      <h2 class="text-xl font-semibold">Documentation</h2>
      <p class="text-gray-500 dark:text-gray-400">Learn how to use Basepod</p>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-4 gap-6">
      <!-- Sidebar Navigation -->
      <div class="lg:col-span-1">
        <UCard>
          <nav class="space-y-1">
            <button
              v-for="section in sections"
              :key="section.id"
              class="w-full flex items-center gap-3 px-3 py-2 text-sm rounded-md transition-colors"
              :class="activeSection === section.id
                ? 'bg-primary-50 dark:bg-primary-900/20 text-primary-600 dark:text-primary-400'
                : 'text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-(--ui-bg-elevated)'"
              @click="activeSection = section.id"
            >
              <UIcon :name="section.icon" class="w-5 h-5" />
              {{ section.title }}
            </button>
          </nav>
        </UCard>
      </div>

      <!-- Content -->
      <div class="lg:col-span-3">
        <UCard>
          <!-- Getting Started -->
          <div v-show="activeSection === 'getting-started'" class="docs-content">
            <h2 class="text-2xl font-bold mb-2">Getting Started</h2>
            <p class="text-(--ui-text-muted) mb-6">Basepod is a lightweight PaaS (Platform as a Service) that makes it easy to deploy and manage containerized applications.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Key Features</h3>
            <ul class="space-y-2 ml-5 list-disc text-(--ui-text-muted)">
              <li><strong class="text-(--ui-text)">One-Click Apps</strong> — Deploy popular apps like PostgreSQL, Redis, WordPress with a single click</li>
              <li><strong class="text-(--ui-text)">Automatic SSL</strong> — Free SSL certificates via Caddy with automatic renewal</li>
              <li><strong class="text-(--ui-text)">Container Management</strong> — Start, stop, restart, and monitor your apps</li>
              <li><strong class="text-(--ui-text)">Environment Variables</strong> — Configure apps with custom environment variables</li>
              <li><strong class="text-(--ui-text)">Persistent Storage</strong> — Automatic volume management for data persistence</li>
            </ul>

            <h3 class="text-lg font-semibold mt-6 mb-3">Quick Start</h3>
            <ol class="space-y-2 ml-5 list-decimal text-(--ui-text-muted)">
              <li>Go to <strong class="text-(--ui-text)">One-Click Apps</strong> in the sidebar</li>
              <li>Select a template (e.g., PostgreSQL)</li>
              <li>Configure the app name and settings</li>
              <li>Click <strong class="text-(--ui-text)">Create</strong></li>
              <li>Wait up to 60 seconds for the app to start</li>
            </ol>
          </div>

          <!-- Managing Apps -->
          <div v-show="activeSection === 'apps'" class="docs-content">
            <h2 class="text-2xl font-bold mb-2">Managing Apps</h2>
            <p class="text-(--ui-text-muted) mb-6">The Apps page shows all your deployed applications with their current status.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">App Status</h3>
            <ul class="space-y-2 ml-5 list-disc text-(--ui-text-muted)">
              <li><strong class="text-(--ui-text)">Running</strong> — App is active and healthy</li>
              <li><strong class="text-(--ui-text)">Stopped</strong> — App is not running</li>
              <li><strong class="text-(--ui-text)">Exited</strong> — App stopped unexpectedly</li>
            </ul>

            <h3 class="text-lg font-semibold mt-6 mb-3">App Actions</h3>
            <ul class="space-y-2 ml-5 list-disc text-(--ui-text-muted)">
              <li><strong class="text-(--ui-text)">Start</strong> — Start a stopped app</li>
              <li><strong class="text-(--ui-text)">Stop</strong> — Stop a running app</li>
              <li><strong class="text-(--ui-text)">Restart</strong> — Restart an app (useful after config changes)</li>
              <li><strong class="text-(--ui-text)">Logs</strong> — View application logs for debugging</li>
              <li><strong class="text-(--ui-text)">Delete</strong> — Remove an app and its container</li>
            </ul>

            <h3 class="text-lg font-semibold mt-6 mb-3">Environment Variables</h3>
            <p class="text-(--ui-text-muted)">Each app can have custom environment variables. Edit them in the app detail page and click "Save and Restart" to apply changes.</p>
          </div>

          <!-- Templates -->
          <div v-show="activeSection === 'templates'" class="docs-content">
            <h2 class="text-2xl font-bold mb-2">One-Click Apps</h2>
            <p class="text-(--ui-text-muted) mb-6">Deploy popular applications with pre-configured settings.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Available Categories</h3>
            <ul class="space-y-2 ml-5 list-disc text-(--ui-text-muted)">
              <li><strong class="text-(--ui-text)">Databases</strong> — PostgreSQL, MySQL, MariaDB, MongoDB, Redis</li>
              <li><strong class="text-(--ui-text)">Admin Tools</strong> — phpMyAdmin, Adminer, pgAdmin</li>
              <li><strong class="text-(--ui-text)">Web Servers</strong> — Nginx, Apache, Caddy</li>
              <li><strong class="text-(--ui-text)">CMS</strong> — WordPress, Ghost, Strapi</li>
              <li><strong class="text-(--ui-text)">Dev Tools</strong> — Gitea, Portainer, Uptime Kuma, Code Server</li>
              <li><strong class="text-(--ui-text)">Storage</strong> — MinIO, File Browser</li>
            </ul>

            <h3 class="text-lg font-semibold mt-6 mb-3">Version Selection</h3>
            <p class="text-(--ui-text-muted)">Each template supports multiple versions. You can search for specific tags (e.g., "alpine") to find lightweight variants.</p>
          </div>

          <!-- Databases -->
          <div v-show="activeSection === 'databases'" class="docs-content">
            <h2 class="text-2xl font-bold mb-2">Databases</h2>
            <p class="text-(--ui-text-muted) mb-6">Database containers are configured for internal access by default.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Connection Info</h3>
            <p class="text-(--ui-text-muted) mb-3">After deploying a database, view the connection details in the app detail page:</p>
            <ul class="space-y-2 ml-5 list-disc text-(--ui-text-muted)">
              <li><strong class="text-(--ui-text)">Internal Host</strong> — Use this for container-to-container connections (e.g., <code class="bg-(--ui-bg-muted) px-1.5 py-0.5 rounded text-sm font-mono">basepod-postgres:5432</code>)</li>
              <li><strong class="text-(--ui-text)">Credentials</strong> — Username, password, and database name from environment variables</li>
            </ul>

            <h3 class="text-lg font-semibold mt-6 mb-3">External Access</h3>
            <p class="text-(--ui-text-muted) mb-4">Enable "External Access" when deploying to expose the database port to the host. The assigned port will appear in the connection info after deployment.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Default Credentials</h3>
            <div class="border border-(--ui-border) rounded-lg overflow-hidden">
              <table class="w-full text-sm">
                <thead>
                  <tr class="bg-(--ui-bg-muted) border-b border-(--ui-border)">
                    <th class="text-left px-4 py-2.5 font-medium">Database</th>
                    <th class="text-left px-4 py-2.5 font-medium">Username</th>
                    <th class="text-left px-4 py-2.5 font-medium">Default Password</th>
                  </tr>
                </thead>
                <tbody class="text-(--ui-text-muted)">
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2.5">PostgreSQL</td><td class="px-4 py-2.5 font-mono">postgres</td><td class="px-4 py-2.5 font-mono">changeme</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2.5">MySQL/MariaDB</td><td class="px-4 py-2.5 font-mono">root</td><td class="px-4 py-2.5 font-mono">changeme</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2.5">MongoDB</td><td class="px-4 py-2.5 font-mono">admin</td><td class="px-4 py-2.5 font-mono">changeme</td></tr>
                  <tr><td class="px-4 py-2.5">Redis</td><td class="px-4 py-2.5 font-mono">—</td><td class="px-4 py-2.5 font-mono">changeme</td></tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- Domains & SSL -->
          <div v-show="activeSection === 'domains'" class="docs-content">
            <h2 class="text-2xl font-bold mb-2">Domains & SSL</h2>
            <p class="text-(--ui-text-muted) mb-6">Basepod uses Caddy as a reverse proxy with automatic SSL.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Automatic Subdomains</h3>
            <p class="text-(--ui-text-muted) mb-4">Each web app gets a subdomain based on its name. For example, if your domain is <code class="bg-(--ui-bg-muted) px-1.5 py-0.5 rounded text-sm font-mono">example.com</code> and you create an app named "blog", it will be accessible at <code class="bg-(--ui-bg-muted) px-1.5 py-0.5 rounded text-sm font-mono">blog.example.com</code>.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">SSL Certificates</h3>
            <p class="text-(--ui-text-muted) mb-4">Caddy automatically obtains and renews SSL certificates from Let's Encrypt. No configuration needed.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Domain Settings</h3>
            <p class="text-(--ui-text-muted)">Configure your base domain in <strong class="text-(--ui-text)">Settings > Domain Settings</strong>. Enable wildcard mode for automatic subdomains.</p>
          </div>

          <!-- API Reference -->
          <div v-show="activeSection === 'api'" class="docs-content">
            <h2 class="text-2xl font-bold mb-2">API Reference</h2>
            <p class="text-(--ui-text-muted) mb-6">Basepod provides a comprehensive REST API for programmatic access to all features.</p>

            <h3 class="text-lg font-semibold mt-6 mb-3">Authentication</h3>
            <p class="text-(--ui-text-muted) mb-3">All API requests (except login and health) require a Bearer token or session cookie:</p>
            <pre class="bg-gray-950 text-gray-300 px-4 py-3 rounded-lg font-mono text-sm mb-6 overflow-x-auto">Authorization: Bearer YOUR_TOKEN</pre>

            <h3 class="text-lg font-semibold mt-6 mb-3">Interactive API Docs</h3>
            <p class="text-(--ui-text-muted) mb-4">Browse all endpoints with parameters, types, and example requests:</p>
            <NuxtLink to="/docs/api">
              <UButton icon="i-heroicons-code-bracket" size="lg">
                Open API Reference
              </UButton>
            </NuxtLink>

            <h3 class="text-lg font-semibold mt-8 mb-3">Quick Overview</h3>
            <div class="border border-(--ui-border) rounded-lg overflow-hidden">
              <table class="w-full text-sm">
                <thead>
                  <tr class="bg-(--ui-bg-muted) border-b border-(--ui-border)">
                    <th class="text-left px-4 py-2.5 font-medium">Category</th>
                    <th class="text-left px-4 py-2.5 font-medium w-20">Endpoints</th>
                    <th class="text-left px-4 py-2.5 font-medium">Description</th>
                  </tr>
                </thead>
                <tbody class="text-(--ui-text-muted)">
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Auth</td><td class="px-4 py-2">6</td><td class="px-4 py-2">Login, logout, setup, invite, password management</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Apps</td><td class="px-4 py-2">12</td><td class="px-4 py-2">CRUD, start, stop, restart, deploy, logs, terminal</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Templates</td><td class="px-4 py-2">2</td><td class="px-4 py-2">List and deploy one-click app templates</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Health & Metrics</td><td class="px-4 py-2">4</td><td class="px-4 py-2">Health checks, resource usage, access logs</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Webhooks</td><td class="px-4 py-2">3</td><td class="px-4 py-2">GitHub auto-deploy webhook setup</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Cron Jobs</td><td class="px-4 py-2">6</td><td class="px-4 py-2">Scheduled task management</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">MLX / LLM</td><td class="px-4 py-2">8</td><td class="px-4 py-2">Local LLM model management</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Storage</td><td class="px-4 py-2">8</td><td class="px-4 py-2">Disk usage, volumes, images</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">System</td><td class="px-4 py-2">9</td><td class="px-4 py-2">Info, config, version, update, prune</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Backups</td><td class="px-4 py-2">6</td><td class="px-4 py-2">Create, download, restore, delete</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Users</td><td class="px-4 py-2">4</td><td class="px-4 py-2">User management and invitations</td></tr>
                  <tr class="border-b border-(--ui-border)"><td class="px-4 py-2">Notifications</td><td class="px-4 py-2">5</td><td class="px-4 py-2">Webhook/Slack/Discord hooks</td></tr>
                  <tr><td class="px-4 py-2">Deploy Tokens</td><td class="px-4 py-2">3</td><td class="px-4 py-2">API token management for CI/CD</td></tr>
                </tbody>
              </table>
            </div>

            <h3 class="text-lg font-semibold mt-8 mb-3">GitHub</h3>
            <p class="text-(--ui-text-muted)">For more details, visit the <a href="https://github.com/base-go/basepod" target="_blank" class="text-primary-500 hover:underline">GitHub repository</a>.</p>
          </div>
        </UCard>
      </div>
    </div>
  </div>
</template>
