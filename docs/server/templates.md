# One-Click Templates

Deploy pre-configured applications with a single command.

The catalog is curated for apps that Basepod can run cleanly as single managed services. Reverse proxies, container-engine managers, and templates that need a separately wired peer service are intentionally excluded.

## Available Templates

### Databases

| Name | Description | Default Port |
|------|-------------|--------------|
| `mysql` | MySQL Database Server | 3306 |
| `mariadb` | MariaDB Database Server | 3306 |
| `postgres` | PostgreSQL Database | 5432 |
| `mongodb` | MongoDB NoSQL Database | 27017 |
| `redis` | Redis Cache/Store | 6379 |

### Admin Tools

| Name | Description | Default Port |
|------|-------------|--------------|
| `phpmyadmin` | MySQL Web Admin | 80 |
| `adminer` | Universal DB Admin | 8080 |
| `pgadmin` | PostgreSQL Admin | 80 |

### CMS & Content

| Name | Description | Default Port |
|------|-------------|--------------|
| `ghost` | Ghost Publishing | 2368 |
| `strapi` | Headless CMS | 1337 |
| `nextcloud` | File Sync & Share | 80 |
| `directus` | Headless CMS | 8055 |
| `drupal` | Enterprise CMS | 80 |
| `mediawiki` | Wiki Software | 80 |
| `pocketbase` | Backend in a File | 8080 |

### Development Tools

| Name | Description | Default Port |
|------|-------------|--------------|
| `gitea` | Self-hosted Git | 3000 |
| `uptime-kuma` | Uptime Monitoring | 3001 |
| `code-server` | VS Code in Browser | 8080 |

### Communication

| Name | Description | Default Port |
|------|-------------|--------------|
| `mattermost` | Slack Alternative | 8065 |

### Automation

| Name | Description | Default Port |
|------|-------------|--------------|
| `n8n` | Workflow Automation | 5678 |

### Analytics

| Name | Description | Default Port |
|------|-------------|--------------|
| `grafana` | Metrics Dashboard | 3000 |

### Storage

| Name | Description | Default Port |
|------|-------------|--------------|
| `minio` | S3-Compatible Storage | 9000 |
| `filebrowser` | Web File Manager | 80 |

### Business

| Name | Description | Default Port |
|------|-------------|--------------|
| `nocodb` | Airtable Alternative | 8080 |
| `listmonk` | Newsletter Manager | 9000 |

### AI

| Name | Description | Default Port |
|------|-------------|--------------|
| `ollama` | Local LLM Runtime | 11434 |
| `flowise` | LLM Workflow Builder | 3000 |

### Security

| Name | Description | Default Port |
|------|-------------|--------------|
| `vaultwarden` | Bitwarden-Compatible Vault | 80 |

### Media

| Name | Description | Default Port |
|------|-------------|--------------|
| `jellyfin` | Media Streaming Server | 8096 |

### Search

| Name | Description | Default Port |
|------|-------------|--------------|
| `meilisearch` | Search Engine | 7700 |

### Messaging

| Name | Description | Default Port |
|------|-------------|--------------|
| `rabbitmq` | Message Broker | 15672 |

## Deploy via CLI

### Basic Deploy

```bash
bp template deploy <name>
```

### With Custom Name

```bash
bp template deploy postgres --name mydb
```

### With Environment Variables

```bash
bp template deploy postgres \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_USER=myuser \
  -e POSTGRES_DB=myapp
```

### Specific Version

```bash
bp template deploy postgres --version 16
bp template deploy redis --version 7
```

## Deploy via Web UI

1. Go to **Templates** in the dashboard
2. Find your template
3. Click **Deploy**
4. Configure name and environment variables
5. Click **Create**

## Template Details

### PostgreSQL

```bash
bp template deploy postgres \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_DB=postgres
```

**Volumes:** `/var/lib/postgresql/data`

**Versions:** 17, 16, 15, 14, 13 (alpine variants available)

### MySQL

```bash
bp template deploy mysql \
  -e MYSQL_ROOT_PASSWORD=secret \
  -e MYSQL_DATABASE=myapp
```

**Volumes:** `/var/lib/mysql`

**Versions:** 8.4, 8.0, 5.7

### Redis

```bash
bp template deploy redis
```

**Volumes:** `/data`

**Versions:** 7, 6 (alpine variants available)

### Gitea

```bash
bp template deploy gitea
```

**Volumes:** `/data`

**Ports:** 3000 (HTTP), 22 (SSH)

### n8n

```bash
bp template deploy n8n \
  -e N8N_BASIC_AUTH_USER=admin \
  -e N8N_BASIC_AUTH_PASSWORD=secret
```

**Volumes:** `/home/node/.n8n`

## Architecture Notes

- **ARM64 (Apple Silicon):** All templates work
- **AMD64 (Intel/AMD):** All templates work
- Some templates have architecture-specific images (e.g., Strapi is amd64-only)
