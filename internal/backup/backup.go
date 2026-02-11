// Package backup provides backup and restore functionality for basepod.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/base-go/basepod/internal/config"
	"github.com/base-go/basepod/internal/podman"
)

// Backup represents a backup archive with metadata
type Backup struct {
	ID        string    `json:"id"`         // Timestamp-based ID (e.g., 20260130-151200)
	CreatedAt time.Time `json:"created_at"` // When backup was created
	Size      int64     `json:"size"`       // Size in bytes
	Path      string    `json:"path"`       // Full path to backup file
	Contents  Contents  `json:"contents"`   // What's included in backup
}

// Contents describes what's in the backup
type Contents struct {
	Database     bool     `json:"database"`      // basepod.db included
	Config       bool     `json:"config"`        // Config files included
	StaticSites  []string `json:"static_sites"`  // List of static sites backed up
	Volumes      []string `json:"volumes"`       // List of volumes backed up
	AppsMetadata int      `json:"apps_metadata"` // Number of apps in database
}

// Options for creating a backup
type Options struct {
	OutputDir      string // Where to save backup (default: /usr/local/basepod/backups)
	IncludeVolumes bool   // Include container volumes (default: true)
	IncludeBuilds  bool   // Include build sources (default: false)
}

// DefaultOptions returns sensible defaults for backup
func DefaultOptions() Options {
	return Options{
		IncludeVolumes: true,
		IncludeBuilds:  false,
	}
}

// Service handles backup operations
type Service struct {
	paths  *config.Paths
	podman podman.Client
}

// NewService creates a new backup service
func NewService(paths *config.Paths, podmanClient podman.Client) *Service {
	return &Service{
		paths:  paths,
		podman: podmanClient,
	}
}

// Create creates a new backup archive
func (s *Service) Create(ctx context.Context, opts Options) (*Backup, error) {
	// Generate backup ID based on timestamp
	now := time.Now()
	backupID := now.Format("20060102-150405")

	// Determine output directory
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(s.paths.Base, "backups")
	}

	// Ensure backup directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	backupPath := filepath.Join(outputDir, fmt.Sprintf("basepod-backup-%s.tar.gz", backupID))

	// Create the backup file
	file, err := os.Create(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	contents := Contents{}

	// 1. Backup database
	dbPath := filepath.Join(s.paths.Data, "basepod.db")
	if _, err := os.Stat(dbPath); err == nil {
		if err := s.addFileToTar(tarWriter, dbPath, "database/basepod.db"); err != nil {
			return nil, fmt.Errorf("failed to backup database: %w", err)
		}
		contents.Database = true
	}

	// 2. Backup config files
	configFiles := []string{"basepod.yaml", "Caddyfile"}
	for _, cf := range configFiles {
		cfPath := filepath.Join(s.paths.Config, cf)
		if _, err := os.Stat(cfPath); err == nil {
			if err := s.addFileToTar(tarWriter, cfPath, "config/"+cf); err != nil {
				return nil, fmt.Errorf("failed to backup config %s: %w", cf, err)
			}
			contents.Config = true
		}
	}

	// 3. Backup static sites
	appsDir := s.paths.Apps
	if entries, err := os.ReadDir(appsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				appPath := filepath.Join(appsDir, entry.Name())
				tarPath := "apps/" + entry.Name()
				if err := s.addDirToTar(tarWriter, appPath, tarPath); err != nil {
					return nil, fmt.Errorf("failed to backup app %s: %w", entry.Name(), err)
				}
				contents.StaticSites = append(contents.StaticSites, entry.Name())
			}
		}
	}

	// 4. Backup container volumes
	if opts.IncludeVolumes && s.podman != nil {
		volumes, err := s.podman.ListVolumes(ctx)
		if err == nil {
			for _, vol := range volumes {
				// Only backup basepod-related volumes
				if strings.HasPrefix(vol.Name, "basepod-") || strings.Contains(vol.Name, "-data") {
					volData, err := s.exportVolume(ctx, vol.Name)
					if err != nil {
						// Log warning but continue
						fmt.Printf("Warning: failed to export volume %s: %v\n", vol.Name, err)
						continue
					}
					if len(volData) > 0 {
						header := &tar.Header{
							Name:    "volumes/" + vol.Name + ".tar",
							Size:    int64(len(volData)),
							Mode:    0644,
							ModTime: time.Now(),
						}
						if err := tarWriter.WriteHeader(header); err != nil {
							return nil, fmt.Errorf("failed to write volume header: %w", err)
						}
						if _, err := tarWriter.Write(volData); err != nil {
							return nil, fmt.Errorf("failed to write volume data: %w", err)
						}
						contents.Volumes = append(contents.Volumes, vol.Name)
					}
				}
			}
		}
	}

	// 5. Backup builds (optional)
	if opts.IncludeBuilds {
		buildsDir := filepath.Join(s.paths.Base, "builds")
		if _, err := os.Stat(buildsDir); err == nil {
			if err := s.addDirToTar(tarWriter, buildsDir, "builds"); err != nil {
				return nil, fmt.Errorf("failed to backup builds: %w", err)
			}
		}
	}

	// 6. Write metadata
	metadata := Backup{
		ID:        backupID,
		CreatedAt: now,
		Contents:  contents,
	}
	metadataJSON, _ := json.MarshalIndent(metadata, "", "  ")
	metaHeader := &tar.Header{
		Name:    "backup.json",
		Size:    int64(len(metadataJSON)),
		Mode:    0644,
		ModTime: now,
	}
	if err := tarWriter.WriteHeader(metaHeader); err != nil {
		return nil, fmt.Errorf("failed to write metadata header: %w", err)
	}
	if _, err := tarWriter.Write(metadataJSON); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	// Close writers to flush data
	tarWriter.Close()
	gzWriter.Close()
	file.Close()

	// Get final file size
	fi, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat backup file: %w", err)
	}

	return &Backup{
		ID:        backupID,
		CreatedAt: now,
		Size:      fi.Size(),
		Path:      backupPath,
		Contents:  contents,
	}, nil
}

// List returns all available backups
func (s *Service) List() ([]Backup, error) {
	backupsDir := filepath.Join(s.paths.Base, "backups")

	// Check if backups directory exists
	if _, err := os.Stat(backupsDir); os.IsNotExist(err) {
		return []Backup{}, nil
	}

	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backups directory: %w", err)
	}

	var backups []Backup
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}

		path := filepath.Join(backupsDir, entry.Name())
		fi, err := entry.Info()
		if err != nil {
			continue
		}

		// Extract ID from filename (basepod-backup-YYYYMMDD-HHMMSS.tar.gz)
		name := entry.Name()
		id := strings.TrimPrefix(name, "basepod-backup-")
		id = strings.TrimSuffix(id, ".tar.gz")

		// Try to read metadata from backup
		contents, createdAt := s.readBackupMetadata(path)
		if createdAt.IsZero() {
			createdAt = fi.ModTime()
		}

		backups = append(backups, Backup{
			ID:        id,
			CreatedAt: createdAt,
			Size:      fi.Size(),
			Path:      path,
			Contents:  contents,
		})
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// Get retrieves a specific backup by ID
func (s *Service) Get(id string) (*Backup, error) {
	backups, err := s.List()
	if err != nil {
		return nil, err
	}

	for _, b := range backups {
		if b.ID == id {
			return &b, nil
		}
	}

	return nil, fmt.Errorf("backup not found: %s", id)
}

// Delete removes a backup
func (s *Service) Delete(id string) error {
	backup, err := s.Get(id)
	if err != nil {
		return err
	}

	return os.Remove(backup.Path)
}

// RestoreOptions configures what to restore
type RestoreOptions struct {
	RestoreDatabase bool // Restore database (default: true)
	RestoreConfig   bool // Restore config files (default: true)
	RestoreApps     bool // Restore static sites (default: true)
	RestoreVolumes  bool // Restore container volumes (default: true)
}

// DefaultRestoreOptions returns sensible defaults for restore
func DefaultRestoreOptions() RestoreOptions {
	return RestoreOptions{
		RestoreDatabase: true,
		RestoreConfig:   true,
		RestoreApps:     true,
		RestoreVolumes:  true,
	}
}

// RestoreResult contains information about what was restored
type RestoreResult struct {
	Database     bool     `json:"database"`
	ConfigFiles  []string `json:"config_files"`
	StaticSites  []string `json:"static_sites"`
	Volumes      []string `json:"volumes"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Restore restores from a backup archive
func (s *Service) Restore(ctx context.Context, id string, opts RestoreOptions) (*RestoreResult, error) {
	// Get backup info
	backup, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	return s.RestoreFromPath(ctx, backup.Path, opts)
}

// RestoreFromPath restores from a backup file path (for uploaded backups)
func (s *Service) RestoreFromPath(ctx context.Context, backupPath string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{}

	// Open backup file
	file, err := os.Open(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	// Process each file in the archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		// Skip directories, we'll create them as needed
		if header.Typeflag == tar.TypeDir {
			continue
		}

		// Route to appropriate restore handler
		switch {
		case header.Name == "backup.json":
			// Skip metadata file
			continue

		case strings.HasPrefix(header.Name, "database/") && opts.RestoreDatabase:
			if err := s.restoreDatabase(tarReader, header); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("database: %v", err))
			} else {
				result.Database = true
			}

		case strings.HasPrefix(header.Name, "config/") && opts.RestoreConfig:
			filename := strings.TrimPrefix(header.Name, "config/")
			if err := s.restoreConfig(tarReader, header, filename); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("config %s: %v", filename, err))
			} else {
				result.ConfigFiles = append(result.ConfigFiles, filename)
			}

		case strings.HasPrefix(header.Name, "apps/") && opts.RestoreApps:
			if err := s.restoreApp(tarReader, header); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("app: %v", err))
			} else {
				// Extract app name from path (apps/appname/...)
				parts := strings.SplitN(strings.TrimPrefix(header.Name, "apps/"), "/", 2)
				if len(parts) > 0 && !contains(result.StaticSites, parts[0]) {
					result.StaticSites = append(result.StaticSites, parts[0])
				}
			}

		case strings.HasPrefix(header.Name, "volumes/") && opts.RestoreVolumes:
			volumeName := strings.TrimPrefix(header.Name, "volumes/")
			volumeName = strings.TrimSuffix(volumeName, ".tar")
			if err := s.restoreVolume(ctx, tarReader, header, volumeName); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("volume %s: %v", volumeName, err))
			} else {
				result.Volumes = append(result.Volumes, volumeName)
			}
		}
	}

	return result, nil
}

// restoreDatabase restores the SQLite database
func (s *Service) restoreDatabase(r io.Reader, header *tar.Header) error {
	dbPath := filepath.Join(s.paths.Data, "basepod.db")

	// Create backup of current database if it exists
	if _, err := os.Stat(dbPath); err == nil {
		backupPath := dbPath + ".bak." + time.Now().Format("20060102-150405")
		if err := copyFile(dbPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup current database: %w", err)
		}
	}

	// Ensure data directory exists
	if err := os.MkdirAll(s.paths.Data, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Write new database file
	file, err := os.Create(dbPath)
	if err != nil {
		return fmt.Errorf("failed to create database file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, r); err != nil {
		return fmt.Errorf("failed to write database: %w", err)
	}

	return nil
}

// restoreConfig restores a config file
func (s *Service) restoreConfig(r io.Reader, header *tar.Header, filename string) error {
	configPath := filepath.Join(s.paths.Config, filename)

	// Create backup of current config if it exists
	if _, err := os.Stat(configPath); err == nil {
		backupPath := configPath + ".bak." + time.Now().Format("20060102-150405")
		if err := copyFile(configPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup current config: %w", err)
		}
	}

	// Ensure config directory exists
	if err := os.MkdirAll(s.paths.Config, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write new config file
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, r); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Set appropriate permissions
	os.Chmod(configPath, os.FileMode(header.Mode))

	return nil
}

// restoreApp restores a static site file
func (s *Service) restoreApp(r io.Reader, header *tar.Header) error {
	// Get relative path within apps directory
	relPath := strings.TrimPrefix(header.Name, "apps/")
	destPath := filepath.Join(s.paths.Apps, relPath)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Handle directories
	if header.Typeflag == tar.TypeDir {
		return os.MkdirAll(destPath, os.FileMode(header.Mode))
	}

	// Write file
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, r); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Set appropriate permissions
	os.Chmod(destPath, os.FileMode(header.Mode))

	return nil
}

// restoreVolume restores a container volume
func (s *Service) restoreVolume(ctx context.Context, r io.Reader, header *tar.Header, volumeName string) error {
	// Find podman binary
	podmanPath := findPodmanPath()

	// Create a temporary file for the volume tar
	tmpFile, err := os.CreateTemp("", "volume-*.tar")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write volume data to temp file
	if _, err := io.Copy(tmpFile, r); err != nil {
		return fmt.Errorf("failed to write volume data: %w", err)
	}
	tmpFile.Close()

	// Check if volume exists, create if not
	checkCmd := exec.CommandContext(ctx, podmanPath, "volume", "exists", volumeName)
	if err := checkCmd.Run(); err != nil {
		// Volume doesn't exist, create it
		createCmd := exec.CommandContext(ctx, podmanPath, "volume", "create", volumeName)
		if err := createCmd.Run(); err != nil {
			return fmt.Errorf("failed to create volume: %w", err)
		}
	}

	// Import volume data
	importCmd := exec.CommandContext(ctx, podmanPath, "volume", "import", volumeName, tmpFile.Name())
	if output, err := importCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to import volume: %w (output: %s)", err, string(output))
	}

	return nil
}

// Helper functions

func findPodmanPath() string {
	podmanPath := "podman"
	if _, err := exec.LookPath("podman"); err != nil {
		for _, p := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman", "/usr/bin/podman"} {
			if _, err := os.Stat(p); err == nil {
				podmanPath = p
				break
			}
		}
	}
	return podmanPath
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// addFileToTar adds a single file to the tar archive
func (s *Service) addFileToTar(tw *tar.Writer, filePath, tarPath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    tarPath,
		Size:    fi.Size(),
		Mode:    int64(fi.Mode()),
		ModTime: fi.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

// addDirToTar recursively adds a directory to the tar archive
func (s *Service) addDirToTar(tw *tar.Writer, dirPath, tarPath string) error {
	return filepath.Walk(dirPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create relative path for tar
		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		tarFilePath := filepath.Join(tarPath, relPath)

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		header.Name = tarFilePath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If it's a file, copy contents
		if !fi.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// exportVolume exports a podman volume to a tar archive
func (s *Service) exportVolume(ctx context.Context, volumeName string) ([]byte, error) {
	// Find podman binary
	podmanPath := "podman"
	if _, err := exec.LookPath("podman"); err != nil {
		for _, p := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman", "/usr/bin/podman"} {
			if _, err := os.Stat(p); err == nil {
				podmanPath = p
				break
			}
		}
	}

	// Use podman volume export
	cmd := exec.CommandContext(ctx, podmanPath, "volume", "export", volumeName)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("podman volume export failed: %w", err)
	}

	return output, nil
}

// readBackupMetadata reads the backup.json from a backup archive
func (s *Service) readBackupMetadata(backupPath string) (Contents, time.Time) {
	file, err := os.Open(backupPath)
	if err != nil {
		return Contents{}, time.Time{}
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return Contents{}, time.Time{}
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Contents{}, time.Time{}
		}

		if header.Name == "backup.json" {
			var metadata Backup
			if err := json.NewDecoder(tarReader).Decode(&metadata); err != nil {
				return Contents{}, time.Time{}
			}
			return metadata.Contents, metadata.CreatedAt
		}
	}

	return Contents{}, time.Time{}
}

// FormatSize formats bytes to human-readable string
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
