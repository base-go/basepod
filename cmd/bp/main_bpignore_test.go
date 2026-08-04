package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBpignore(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, ".bpignore"), []byte("# comment\nweb/node_modules/\n\n.nuxt/\napi/base\n"), 0o644)
	got := readBpignore(d)
	want := map[string]bool{"web/node_modules": true, ".nuxt": true, "api/base": true}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected pattern %q", g)
		}
	}
	// Absent file → nil, so createTarball falls back to hardcoded defaults.
	if readBpignore(t.TempDir()) != nil {
		t.Error("missing .bpignore should return nil")
	}
}
