// Package imagesync provides Docker Hub image tag synchronization.
package imagesync

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/deployer/deployer/internal/storage"
	"github.com/deployer/deployer/internal/templates"
)

// Syncer handles periodic synchronization of image tags from Docker Hub
type Syncer struct {
	storage  *storage.Storage
	client   *http.Client
	interval time.Duration
	stopCh   chan struct{}
}

// NewSyncer creates a new image tag syncer
func NewSyncer(store *storage.Storage) *Syncer {
	return &Syncer{
		storage:  store,
		client:   &http.Client{Timeout: 30 * time.Second},
		interval: 24 * time.Hour,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the periodic sync process
func (s *Syncer) Start() {
	// Initial sync on startup
	go func() {
		log.Println("Starting initial image tag sync...")
		s.SyncAll()
		log.Println("Initial image tag sync complete")
	}()

	// Periodic sync
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Println("Running periodic image tag sync...")
				s.SyncAll()
				log.Println("Periodic image tag sync complete")
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop stops the syncer
func (s *Syncer) Stop() {
	close(s.stopCh)
}

// SyncAll syncs tags for all template images
func (s *Syncer) SyncAll() {
	tmplList := templates.GetTemplatesForArch()
	for _, tmpl := range tmplList {
		if err := s.SyncImage(tmpl.Image); err != nil {
			log.Printf("Failed to sync tags for %s: %v", tmpl.Image, err)
		}
	}
}

// SyncImage syncs tags for a specific image
func (s *Syncer) SyncImage(image string) error {
	tags, err := s.fetchTagsFromDockerHub(image)
	if err != nil {
		return err
	}

	if len(tags) > 0 {
		return s.storage.SaveImageTags(image, tags)
	}
	return nil
}

// DockerHubTagsResponse represents the Docker Hub API response
type DockerHubTagsResponse struct {
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
	Next string `json:"next"`
}

// fetchTagsFromDockerHub fetches tags from Docker Hub API
func (s *Syncer) fetchTagsFromDockerHub(image string) ([]string, error) {
	// Docker Hub API uses library/ prefix for official images
	repoName := image
	if !strings.Contains(image, "/") {
		repoName = "library/" + image
	}

	var allTags []string
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", repoName)

	// Fetch up to 3 pages (300 tags max)
	for i := 0; i < 3 && url != ""; i++ {
		resp, err := s.client.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tags: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Docker Hub returned status %d", resp.StatusCode)
		}

		var data DockerHubTagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, result := range data.Results {
			allTags = append(allTags, result.Name)
		}

		url = data.Next
	}

	// Sort tags: put common versions first (latest, stable, numeric versions)
	sort.Slice(allTags, func(i, j int) bool {
		return tagPriority(allTags[i]) < tagPriority(allTags[j])
	})

	// Limit to 100 most relevant tags
	if len(allTags) > 100 {
		allTags = allTags[:100]
	}

	return allTags, nil
}

// tagPriority returns a sort priority for a tag (lower = higher priority)
func tagPriority(tag string) int {
	switch {
	case tag == "latest":
		return 0
	case tag == "stable":
		return 1
	case tag == "alpine":
		return 2
	case tag == "slim":
		return 3
	case strings.HasSuffix(tag, "-alpine"):
		return 10
	case strings.HasSuffix(tag, "-slim"):
		return 11
	case isNumericVersion(tag):
		return 20
	default:
		return 100
	}
}

// isNumericVersion checks if a tag looks like a version number
func isNumericVersion(tag string) bool {
	if len(tag) == 0 {
		return false
	}
	// Check if starts with a digit
	return tag[0] >= '0' && tag[0] <= '9'
}

// GetTags returns cached tags for an image, or fetches them if not cached
func (s *Syncer) GetTags(image string) ([]string, error) {
	tags, updatedAt, err := s.storage.GetImageTags(image)
	if err != nil {
		return nil, err
	}

	// If we have cached tags and they're less than 24 hours old, use them
	if len(tags) > 0 && time.Since(updatedAt) < 24*time.Hour {
		return tags, nil
	}

	// Otherwise fetch fresh tags
	freshTags, err := s.fetchTagsFromDockerHub(image)
	if err != nil {
		// If fetch fails but we have cached tags, return those
		if len(tags) > 0 {
			return tags, nil
		}
		return nil, err
	}

	// Save and return fresh tags
	if len(freshTags) > 0 {
		s.storage.SaveImageTags(image, freshTags)
	}

	return freshTags, nil
}
