package api

import (
	"strings"
	"testing"

	"github.com/base-go/basepod/internal/templates"
)

func TestMergedTemplateEnvHardensDirectusSecrets(t *testing.T) {
	t.Parallel()

	tmpl := templates.GetTemplate("directus")
	if tmpl == nil {
		t.Fatal("directus template not found")
	}

	env := mergedTemplateEnv(tmpl, nil, "directus-app", "directus.example.com")

	for key, original := range map[string]string{
		"KEY":            tmpl.Env["KEY"],
		"SECRET":         tmpl.Env["SECRET"],
		"ADMIN_PASSWORD": tmpl.Env["ADMIN_PASSWORD"],
	} {
		if env[key] == "" {
			t.Fatalf("expected %s to be generated", key)
		}
		if env[key] == original {
			t.Fatalf("expected %s to change from template placeholder", key)
		}
		if strings.Contains(strings.ToLower(env[key]), "changeme") || strings.Contains(strings.ToLower(env[key]), "replace-with-random") {
			t.Fatalf("expected %s to be hardened, got %q", key, env[key])
		}
	}
}

func TestMergedTemplateEnvPreservesUserProvidedSecrets(t *testing.T) {
	t.Parallel()

	tmpl := templates.GetTemplate("code-server")
	if tmpl == nil {
		t.Fatal("code-server template not found")
	}

	env := mergedTemplateEnv(tmpl, map[string]string{"PASSWORD": "user-supplied-secret"}, "code-app", "code.example.com")
	if env["PASSWORD"] != "user-supplied-secret" {
		t.Fatalf("expected custom password to be preserved, got %q", env["PASSWORD"])
	}
}

func TestMergedTemplateEnvSetsAppSpecificDatabaseCredentials(t *testing.T) {
	t.Parallel()

	mariadb := templates.GetTemplate("mariadb")
	if mariadb == nil {
		t.Fatal("mariadb template not found")
	}

	env := mergedTemplateEnv(mariadb, nil, "orders", "")
	if env["MARIADB_ROOT_PASSWORD"] == "" || env["MARIADB_ROOT_PASSWORD"] == "changeme" {
		t.Fatalf("expected mariadb root password to be generated, got %q", env["MARIADB_ROOT_PASSWORD"])
	}
	if env["MARIADB_PASSWORD"] == "" || env["MARIADB_PASSWORD"] == "changeme" {
		t.Fatalf("expected mariadb app password to be generated, got %q", env["MARIADB_PASSWORD"])
	}
	if env["MARIADB_USER"] != "basepod" {
		t.Fatalf("expected mariadb user to default to basepod, got %q", env["MARIADB_USER"])
	}
	if env["MARIADB_DATABASE"] != "orders" {
		t.Fatalf("expected mariadb database to default to app name, got %q", env["MARIADB_DATABASE"])
	}

	mongodb := templates.GetTemplate("mongodb")
	if mongodb == nil {
		t.Fatal("mongodb template not found")
	}

	env = mergedTemplateEnv(mongodb, nil, "docs", "")
	if env["MONGO_INITDB_ROOT_PASSWORD"] == "" || env["MONGO_INITDB_ROOT_PASSWORD"] == "changeme" {
		t.Fatalf("expected mongodb root password to be generated, got %q", env["MONGO_INITDB_ROOT_PASSWORD"])
	}
}

func TestMergedTemplateEnvUpdatesTemplateURLsAndCommands(t *testing.T) {
	t.Parallel()

	ghost := templates.GetTemplate("ghost")
	if ghost == nil {
		t.Fatal("ghost template not found")
	}

	env := mergedTemplateEnv(ghost, nil, "ghost-app", "ghost.example.com")
	if env["url"] != "http://ghost.example.com" {
		t.Fatalf("expected ghost URL to follow app domain, got %q", env["url"])
	}

	redis := templates.GetTemplate("redis")
	if redis == nil {
		t.Fatal("redis template not found")
	}

	env = mergedTemplateEnv(redis, nil, "cache", "")
	command := resolveTemplateCommand(redis.Command, env)
	if len(command) < 3 {
		t.Fatalf("expected redis command to be expanded, got %#v", command)
	}
	if command[2] != env["REDIS_PASSWORD"] {
		t.Fatalf("expected redis command password %q, got %q", env["REDIS_PASSWORD"], command[2])
	}
}
