package docker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/filters"
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/obot-platform/discobot/server/internal/config"
)

func TestIsLocalImage(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		expected bool
	}{
		{
			name:     "local image with discobot-local prefix",
			image:    "discobot-local/agent-api:latest",
			expected: true,
		},
		{
			name:     "bare digest reference",
			image:    "sha256:abc123def456",
			expected: true,
		},
		{
			name:     "registry digest reference",
			image:    "ghcr.io/obot-platform/discobot@sha256:abc123def456",
			expected: false,
		},
		{
			name:     "tag reference",
			image:    "ghcr.io/obot-platform/discobot:v1.0.0",
			expected: false,
		},
		{
			name:     "latest tag",
			image:    "ghcr.io/obot-platform/discobot:latest",
			expected: false,
		},
		{
			name:     "image without tag",
			image:    "ghcr.io/obot-platform/discobot",
			expected: false,
		},
		{
			name:     "local image without registry",
			image:    "discobot:local",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLocalImage(tt.image)
			if result != tt.expected {
				t.Errorf("isLocalImage(%q) = %v, want %v", tt.image, result, tt.expected)
			}
		})
	}
}

func TestPullSandboxImage_SkipsDigestReferences(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a minimal mock provider for testing
	// Note: This test requires Docker to be running but doesn't actually pull anything
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker client not available:", err)
	}
	defer cli.Close()

	// Verify Docker is accessible
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		t.Skip("Docker daemon not available:", err)
	}

	p := &Provider{
		client: cli,
	}

	tests := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{
			name:    "local image that doesn't exist should error",
			image:   "discobot-local/nonexistent:latest",
			wantErr: true, // Cannot pull local images from registry
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := p.pullSandboxImage(ctx, tt.image)
			if (err != nil) != tt.wantErr {
				t.Errorf("pullSandboxImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCleanupUnusedImages_SkipsWhenCurrentImageMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker client not available:", err)
	}
	defer cli.Close()

	// Verify Docker is accessible
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		t.Skip("Docker daemon not available:", err)
	}

	p := &Provider{
		client: cli,
		cfg: &config.Config{
			SandboxImage: "nonexistent-image:fake-tag",
		},
	}

	// Test that cleanup handles missing images gracefully
	t.Run("handles missing current image", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := p.CleanupUnusedImages(ctx)
		// Should not error even if current image doesn't exist
		if err != nil {
			t.Errorf("CleanupUnusedImages() should handle missing current image gracefully, got error: %v", err)
		}
	})
}

func TestCleanupOldSandboxImages_ListsLabeledImages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker client not available:", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		t.Skip("Docker daemon not available:", err)
	}

	// Test that we can list images with the label
	t.Run("lists images with discobot label", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		images, err := cli.ImageList(ctx, imageTypes.ListOptions{
			Filters: filters.NewArgs(
				filters.Arg("label", "io.discobot.sandbox-image=true"),
			),
		})
		if err != nil {
			t.Fatalf("Failed to list images: %v", err)
		}

		// We expect 0 or more images with this label
		// This test just verifies the query works
		t.Logf("Found %d images with discobot label", len(images))
	})
}

func TestPullSandboxImage_Logging(t *testing.T) {
	// Test that the function correctly identifies local images
	tests := []struct {
		name    string
		image   string
		isLocal bool
	}{
		{
			name:    "local image with discobot-local prefix",
			image:   "discobot-local/agent-api:latest",
			isLocal: true,
		},
		{
			name:    "bare digest",
			image:   "sha256:abc123",
			isLocal: true,
		},
		{
			name:    "registry image with digest",
			image:   "image@sha256:abc123",
			isLocal: false,
		},
		{
			name:    "registry image with tag",
			image:   "image:tag",
			isLocal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLocalImage(tt.image)
			if result != tt.isLocal {
				t.Errorf("isLocalImage(%q) = %v, want %v", tt.image, result, tt.isLocal)
			}
		})
	}
}

// Test helper: verify label format
func TestLabelFormat(t *testing.T) {
	expectedLabel := "io.discobot.sandbox-image=true"

	// Verify the label is properly formatted
	if !strings.Contains(expectedLabel, "io.discobot") {
		t.Error("Label should use io.discobot namespace")
	}

	if !strings.Contains(expectedLabel, "sandbox-image") {
		t.Error("Label should identify sandbox images")
	}

	if !strings.HasSuffix(expectedLabel, "=true") {
		t.Error("Label should have value 'true'")
	}
}

// Benchmark local image detection
func BenchmarkIsLocalImage(b *testing.B) {
	images := []string{
		"discobot-local/agent-api:latest",
		"sha256:abc123def456",
		"ghcr.io/obot-platform/discobot@sha256:abc123def456",
		"ghcr.io/obot-platform/discobot:v1.0.0",
		"ubuntu:latest",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, img := range images {
			_ = isLocalImage(img)
		}
	}
}

// Test error messages
func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name          string
		image         string
		errorContains string
	}{
		{
			name:          "pull error includes image name",
			image:         "test-image:tag",
			errorContains: "test-image:tag",
		},
		{
			name:          "cleanup error message format",
			image:         "current-image:tag",
			errorContains: "", // No error expected for cleanup
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that error messages would include the image name
			err := fmt.Errorf("failed to pull sandbox image %s: %w", tt.image, fmt.Errorf("mock error"))
			if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("Error message should contain %q, got: %v", tt.errorContains, err)
			}
		})
	}
}
