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
          <div v-show="activeSection === 'getting-started'" class="prose dark:prose-invert max-w-none">
            <h2>Getting Started</h2>
            <p>Basepod is a lightweight PaaS (Platform as a Service) that makes it easy to deploy and manage containerized applications.</p>

            <h3>Key Features</h3>
            <ul>
              <li><strong>One-Click Apps</strong> - Deploy popular apps like PostgreSQL, Redis, WordPress with a single click</li>
              <li><strong>Automatic SSL</strong> - Free SSL certificates via Caddy with automatic renewal</li>
              <li><strong>Container Management</strong> - Start, stop, restart, and monitor your apps</li>
              <li><strong>Environment Variables</strong> - Configure apps with custom environment variables</li>
              <li><strong>Persistent Storage</strong> - Automatic volume management for data persistence</li>
            </ul>

            <h3>Quick Start</h3>
            <ol>
              <li>Go to <strong>One-Click Apps</strong> in the sidebar</li>
              <li>Select a template (e.g., PostgreSQL)</li>
              <li>Configure the app name and settings</li>
              <li>Click <strong>Create</strong></li>
              <li>Wait up to 60 seconds for the app to start</li>
            </ol>
          </div>

          <!-- Managing Apps -->
          <div v-show="activeSection === 'apps'" class="prose dark:prose-invert max-w-none">
            <h2>Managing Apps</h2>
            <p>The Apps page shows all your deployed applications with their current status.</p>

            <h3>App Status</h3>
            <ul>
              <li><strong>Running</strong> - App is active and healthy</li>
              <li><strong>Stopped</strong> - App is not running</li>
              <li><strong>Exited</strong> - App stopped unexpectedly</li>
            </ul>

            <h3>App Actions</h3>
            <ul>
              <li><strong>Start</strong> - Start a stopped app</li>
              <li><strong>Stop</strong> - Stop a running app</li>
              <li><strong>Restart</strong> - Restart an app (useful after config changes)</li>
              <li><strong>Logs</strong> - View application logs for debugging</li>
              <li><strong>Delete</strong> - Remove an app and its container</li>
            </ul>

            <h3>Environment Variables</h3>
            <p>Each app can have custom environment variables. Edit them in the app detail page and click "Save and Restart" to apply changes.</p>
          </div>

          <!-- Templates -->
          <div v-show="activeSection === 'templates'" class="prose dark:prose-invert max-w-none">
            <h2>One-Click Apps</h2>
            <p>Deploy popular applications with pre-configured settings.</p>

            <h3>Available Categories</h3>
            <ul>
              <li><strong>Databases</strong> - PostgreSQL, MySQL, MariaDB, MongoDB, Redis</li>
              <li><strong>Admin Tools</strong> - phpMyAdmin, Adminer, pgAdmin</li>
              <li><strong>Web Servers</strong> - Nginx, Apache, Caddy</li>
              <li><strong>CMS</strong> - WordPress, Ghost, Strapi</li>
              <li><strong>Dev Tools</strong> - Gitea, Portainer, Uptime Kuma, Code Server</li>
              <li><strong>Storage</strong> - MinIO, File Browser</li>
            </ul>

            <h3>Version Selection</h3>
            <p>Each template supports multiple versions. You can search for specific tags (e.g., "alpine") to find lightweight variants.</p>
          </div>

          <!-- Databases -->
          <div v-show="activeSection === 'databases'" class="prose dark:prose-invert max-w-none">
            <h2>Databases</h2>
            <p>Database containers are configured for internal access by default.</p>

            <h3>Connection Info</h3>
            <p>After deploying a database, view the connection details in the app detail page:</p>
            <ul>
              <li><strong>Internal Host</strong> - Use this for container-to-container connections (e.g., <code>basepod-postgres:5432</code>)</li>
              <li><strong>Credentials</strong> - Username, password, and database name from environment variables</li>
            </ul>

            <h3>External Access</h3>
            <p>Enable "External Access" when deploying to expose the database port to the host. The assigned port will appear in the connection info after deployment.</p>

            <h3>Default Credentials</h3>
            <table>
              <thead>
                <tr>
                  <th>Database</th>
                  <th>Username</th>
                  <th>Default Password</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>PostgreSQL</td>
                  <td>postgres</td>
                  <td>changeme</td>
                </tr>
                <tr>
                  <td>MySQL/MariaDB</td>
                  <td>root</td>
                  <td>changeme</td>
                </tr>
                <tr>
                  <td>MongoDB</td>
                  <td>admin</td>
                  <td>changeme</td>
                </tr>
                <tr>
                  <td>Redis</td>
                  <td>-</td>
                  <td>changeme</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Domains & SSL -->
          <div v-show="activeSection === 'domains'" class="prose dark:prose-invert max-w-none">
            <h2>Domains & SSL</h2>
            <p>Basepod uses Caddy as a reverse proxy with automatic SSL.</p>

            <h3>Automatic Subdomains</h3>
            <p>Each web app gets a subdomain based on its name. For example, if your domain is <code>example.com</code> and you create an app named "blog", it will be accessible at <code>blog.example.com</code>.</p>

            <h3>SSL Certificates</h3>
            <p>Caddy automatically obtains and renews SSL certificates from Let's Encrypt. No configuration needed.</p>

            <h3>Domain Settings</h3>
            <p>Configure your base domain in <strong>Settings > Domain Settings</strong>. Enable wildcard mode for automatic subdomains.</p>
          </div>

          <!-- API Reference -->
          <div v-show="activeSection === 'api'" class="prose dark:prose-invert max-w-none">
            <h2>API Reference</h2>
            <p>Basepod provides a comprehensive REST API for programmatic access to all features.</p>

            <h3>Authentication</h3>
            <p>All API requests (except login and health) require a Bearer token or session cookie:</p>
            <pre><code>Authorization: Bearer YOUR_TOKEN</code></pre>

            <h3>Interactive API Docs</h3>
            <p>Browse all endpoints with parameters, types, and example requests:</p>
            <NuxtLink to="/docs/api" class="inline-flex items-center gap-2 px-4 py-2 bg-primary-500 text-white rounded-lg hover:bg-primary-600 no-underline transition-colors">
              Open API Reference
            </NuxtLink>

            <h3>Quick Overview</h3>
            <table>
              <thead>
                <tr>
                  <th>Category</th>
                  <th>Endpoints</th>
                  <th>Description</th>
                </tr>
              </thead>
              <tbody>
                <tr><td>Auth</td><td>6</td><td>Login, logout, setup, invite, password management</td></tr>
                <tr><td>Apps</td><td>12</td><td>CRUD, start, stop, restart, deploy, logs, terminal</td></tr>
                <tr><td>Templates</td><td>2</td><td>List and deploy one-click app templates</td></tr>
                <tr><td>Health & Metrics</td><td>4</td><td>Health checks, resource usage, access logs</td></tr>
                <tr><td>Webhooks</td><td>3</td><td>GitHub auto-deploy webhook setup and delivery tracking</td></tr>
                <tr><td>Cron Jobs</td><td>6</td><td>Scheduled task management and execution history</td></tr>
                <tr><td>MLX / LLM</td><td>8</td><td>Local LLM model management on Apple Silicon</td></tr>
                <tr><td>Storage</td><td>8</td><td>Disk usage, volumes, images, LLM storage</td></tr>
                <tr><td>System</td><td>9</td><td>Info, config, version, update, prune, restart</td></tr>
                <tr><td>Backups</td><td>6</td><td>Create, download, restore, delete backups</td></tr>
                <tr><td>Users</td><td>4</td><td>User management and invitations (admin)</td></tr>
                <tr><td>Notifications</td><td>5</td><td>Webhook/Slack/Discord notification hooks</td></tr>
                <tr><td>Deploy Tokens</td><td>3</td><td>API token management for CI/CD</td></tr>
              </tbody>
            </table>

            <h3>GitHub</h3>
            <p>For more details, visit the <a href="https://github.com/base-go/basepod" target="_blank">GitHub repository</a>.</p>
          </div>
        </UCard>
      </div>
    </div>
  </div>
</template>

<style scoped>
.prose table {
  width: 100%;
  font-size: 0.875rem;
}
.prose th, .prose td {
  padding: 0.5rem 0.75rem;
  text-align: left;
  border-bottom: 1px solid var(--ui-border);
}
.prose code {
  background: var(--ui-bg-elevated);
  padding: 0.125rem 0.375rem;
  border-radius: 0.25rem;
  font-size: 0.875rem;
}
.prose pre {
  background: var(--ui-bg-elevated);
  padding: 1rem;
  border-radius: 0.5rem;
  overflow-x: auto;
}
.prose pre code {
  background: transparent;
  padding: 0;
}
</style>
