// Package diskutil provides shared disk usage utilities.
package diskutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DiskUsage holds filesystem-level disk usage info.
type DiskUsage struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Percent   float64 `json:"percent"`
	Formatted struct {
		Total     string `json:"total"`
		Used      string `json:"used"`
		Available string `json:"available"`
	} `json:"formatted"`
}

// GetDiskUsage returns filesystem disk usage for the given path.
func GetDiskUsage(path string) (*DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("statfs %s: %w", path, err)
	}

	total := stat.Blocks * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	used := total - (stat.Bfree * uint64(stat.Bsize))

	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}

	du := &DiskUsage{
		Total:     total,
		Used:      used,
		Available: available,
		Percent:   pct,
	}
	du.Formatted.Total = FormatBytes(int64(total))
	du.Formatted.Used = FormatBytes(int64(used))
	du.Formatted.Available = FormatBytes(int64(available))
	return du, nil
}

// DirSize returns the total size in bytes of all files under path.
func DirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// FileSize returns the size of a single file, or 0 if it cannot be read.
func FileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// FormatBytes converts bytes to a human-readable string.
func FormatBytes(bytes int64) string {
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
