// Package web provides static files for the web UI.
package web

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed dist/*
var embeddedFiles embed.FS

// configuredPath is set by SetWebUIPath to use disk-based serving
var configuredPath string

// SetWebUIPath sets the path to serve static files from disk
// If empty, will use auto-detection or fall back to embedded files
func SetWebUIPath(path string) {
	configuredPath = path
}

// GetFileSystem returns the filesystem for static files.
// Priority: 1) Configured path, 2) Auto-detect disk locations, 3) Embedded files
func GetFileSystem() (fs.FS, string, error) {
	// 1. Use configured path if set
	if configuredPath != "" {
		if info, err := os.Stat(configuredPath); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(configuredPath, "index.html")); err == nil {
				return os.DirFS(configuredPath), "configured: " + configuredPath, nil
			}
		}
	}

	// 2. Auto-detect common disk locations
	diskPaths := []string{
		"/opt/basepod/web",        // Production: synced frontend
		"./web/.output/public",     // Development: nuxt output
		"./internal/web/dist",      // Development: embedded dir
	}

	for _, path := range diskPaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				return os.DirFS(path), "disk: " + path, nil
			}
		}
	}

	// 3. Fallback to embedded files
	fsys, err := fs.Sub(embeddedFiles, "dist")
	return fsys, "embedded", err
}
