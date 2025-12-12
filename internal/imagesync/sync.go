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

// fetchTagsFromDockerHub fetches tags from Docker Hub API or other registries
func (s *Syncer) fetchTagsFromDockerHub(image string) ([]string, error) {
	// Handle different registries
	if strings.HasPrefix(image, "ghcr.io/") {
		return s.fetchTagsFromGHCR(image)
	}
	if strings.HasPrefix(image, "quay.io/") {
		// Skip quay.io for now - would need different API
		return nil, nil
	}

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

// GHCRTagsResponse represents the GitHub Container Registry API response
type GHCRTagsResponse struct {
	Tags []string `json:"tags"`
}

// fetchTagsFromGHCR fetches tags from GitHub Container Registry
func (s *Syncer) fetchTagsFromGHCR(image string) ([]string, error) {
	// ghcr.io/owner/repo -> owner/repo
	repoName := strings.TrimPrefix(image, "ghcr.io/")

	// GHCR uses Docker Registry v2 API
	url := fmt.Sprintf("https://ghcr.io/v2/%s/tags/list", repoName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// GHCR requires accepting the manifest types
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GHCR tags: %w", err)
	}
	defer resp.Body.Close()

	// GHCR returns 401 for public repos without token, but we can still try
	if resp.StatusCode == http.StatusUnauthorized {
		// Try to get anonymous token
		return s.fetchTagsFromGHCRWithToken(repoName)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GHCR returned status %d", resp.StatusCode)
	}

	var data GHCRTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode GHCR response: %w", err)
	}

	// Sort tags
	sort.Slice(data.Tags, func(i, j int) bool {
		return tagPriority(data.Tags[i]) < tagPriority(data.Tags[j])
	})

	// Limit to 100 tags
	if len(data.Tags) > 100 {
		data.Tags = data.Tags[:100]
	}

	return data.Tags, nil
}

// GHCRTokenResponse represents the GHCR token response
type GHCRTokenResponse struct {
	Token string `json:"token"`
}

// fetchTagsFromGHCRWithToken fetches tags using anonymous token
func (s *Syncer) fetchTagsFromGHCRWithToken(repoName string) ([]string, error) {
	// Get anonymous token
	tokenURL := fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull", repoName)
	tokenResp, err := s.client.Get(tokenURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get GHCR token: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		// Skip this image - likely private or doesn't exist
		return nil, nil
	}

	var tokenData GHCRTokenResponse
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return nil, fmt.Errorf("failed to decode GHCR token: %w", err)
	}

	// Now fetch tags with token
	url := fmt.Sprintf("https://ghcr.io/v2/%s/tags/list", repoName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tokenData.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GHCR tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil // Skip - image may not exist
	}

	var data GHCRTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode GHCR response: %w", err)
	}

	// Sort tags
	sort.Slice(data.Tags, func(i, j int) bool {
		return tagPriority(data.Tags[i]) < tagPriority(data.Tags[j])
	})

	// Limit to 100 tags
	if len(data.Tags) > 100 {
		data.Tags = data.Tags[:100]
	}

	return data.Tags, nil
}
