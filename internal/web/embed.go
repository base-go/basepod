// Package web provides embedded static files for the web UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var embeddedFiles embed.FS

// GetFileSystem returns the embedded filesystem with dist prefix stripped
func GetFileSystem() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "dist")
}
