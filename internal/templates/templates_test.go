package templates

import "testing"

func TestCatalogExcludesTemplatesThatDoNotFitBasepod(t *testing.T) {
	t.Parallel()

	unsupported := []string{
		"bookstack",
		"cal",
		"chatwoot",
		"immich",
		"invoiceninja",
		"openwebui",
		"plausible",
		"portainer",
		"rocketchat",
		"traefik",
		"wireguard",
		"wordpress",
	}

	for _, id := range unsupported {
		if tmpl := GetTemplate(id); tmpl != nil {
			t.Fatalf("template %q should not be exposed in the one-click catalog", id)
		}
	}
}

func TestPostgresTemplateUsesVersionCompatibleDataPath(t *testing.T) {
	t.Parallel()

	tmpl := GetTemplate("postgres")
	if tmpl == nil {
		t.Fatal("postgres template not found")
	}

	if tmpl.DefaultVersion != "17" {
		t.Fatalf("expected postgres template to stay pinned to 17 by default, got %q", tmpl.DefaultVersion)
	}

	if len(tmpl.Volumes) != 1 {
		t.Fatalf("expected postgres template to have exactly one data volume, got %d", len(tmpl.Volumes))
	}

	if got := tmpl.Volumes[0].ContainerPath; got != "/var/lib/postgresql/data" {
		t.Fatalf("expected postgres 17 data path /var/lib/postgresql/data, got %q", got)
	}
}
