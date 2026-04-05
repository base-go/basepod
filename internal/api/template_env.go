package api

import (
	"os"
	"strings"

	"github.com/base-go/basepod/internal/templates"
)

func mergedTemplateEnv(tmpl *templates.Template, userEnv map[string]string, appName, domain string) map[string]string {
	env := make(map[string]string, len(tmpl.Env)+len(userEnv))
	for k, v := range tmpl.Env {
		env[k] = v
	}
	for k, v := range userEnv {
		env[k] = v
	}

	if domain != "" {
		targetURL := "http://" + domain
		for _, key := range []string{"url", "APP_URL", "ROOT_URL", "NEXT_PUBLIC_WEBAPP_URL"} {
			if _, ok := env[key]; ok {
				env[key] = targetURL
			}
		}
	}

	hardenTemplateEnv(tmpl.ID, appName, env)
	return env
}

func hardenTemplateEnv(templateID, appName string, env map[string]string) {
	for key, length := range map[string]int{
		"POSTGRES_PASSWORD":          24,
		"MYSQL_ROOT_PASSWORD":        24,
		"MYSQL_PASSWORD":             24,
		"MARIADB_ROOT_PASSWORD":      24,
		"MARIADB_PASSWORD":           24,
		"MONGO_INITDB_ROOT_PASSWORD": 24,
		"REDIS_PASSWORD":             24,
		"PGADMIN_DEFAULT_PASSWORD":   24,
		"ADMIN_PASSWORD":             24,
		"MINIO_ROOT_PASSWORD":        32,
		"PASSWORD":                   24,
		"FLOWISE_PASSWORD":           24,
		"ADMIN_TOKEN":                48,
		"MEILI_MASTER_KEY":           32,
		"RABBITMQ_DEFAULT_PASS":      24,
		"KEY":                        32,
		"SECRET":                     48,
	} {
		if value, ok := env[key]; ok && shouldRegenerateTemplateSecret(value) {
			env[key] = generateRandomString(length)
		}
	}

	switch templateID {
	case "postgres", "postgresql":
		if env["POSTGRES_USER"] == "" {
			env["POSTGRES_USER"] = "basepod"
		}
		if env["POSTGRES_DB"] == "" || env["POSTGRES_DB"] == "app" {
			env["POSTGRES_DB"] = appName
		}
	case "mysql":
		if env["MYSQL_USER"] == "" {
			env["MYSQL_USER"] = "basepod"
		}
		if shouldRegenerateTemplateSecret(env["MYSQL_PASSWORD"]) {
			env["MYSQL_PASSWORD"] = generateRandomString(24)
		}
		if env["MYSQL_DATABASE"] == "" || env["MYSQL_DATABASE"] == "app" {
			env["MYSQL_DATABASE"] = appName
		}
	case "mariadb":
		if env["MARIADB_USER"] == "" {
			env["MARIADB_USER"] = "basepod"
		}
		if shouldRegenerateTemplateSecret(env["MARIADB_PASSWORD"]) {
			env["MARIADB_PASSWORD"] = generateRandomString(24)
		}
		if env["MARIADB_DATABASE"] == "" || env["MARIADB_DATABASE"] == "app" {
			env["MARIADB_DATABASE"] = appName
		}
	}
}

func shouldRegenerateTemplateSecret(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}

	lower := strings.ToLower(value)
	for _, marker := range []string{
		"changeme",
		"replace-with-random",
		"must_be_secret",
		"must-be-secret",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

func resolveTemplateCommand(command []string, env map[string]string) []string {
	if len(command) == 0 {
		return nil
	}

	resolved := make([]string, len(command))
	for i, arg := range command {
		resolved[i] = os.Expand(arg, func(key string) string {
			return env[key]
		})
	}
	return resolved
}
