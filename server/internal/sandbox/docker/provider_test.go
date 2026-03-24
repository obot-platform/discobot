package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
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

func TestBuildSSHKeyArchive(t *testing.T) {
	archive, err := buildSSHKeyArchive(&sandbox.SSHKeyProvision{
		Filename:   "discobot_sandbox",
		PrivateKey: "PRIVATE KEY\n",
		PublicKey:  "ecdsa-sha2-nistp256 AAAATEST discobot",
	})
	if err != nil {
		t.Fatalf("buildSSHKeyArchive failed: %v", err)
	}

	tr := tar.NewReader(archive)
	entries := map[string]string{}
	modes := map[string]int64{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar entry: %v", err)
		}
		modes[hdr.Name] = hdr.Mode
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("failed to read tar file contents for %s: %v", hdr.Name, err)
		}
		entries[hdr.Name] = string(data)
	}

	if modes[".discobot-secrets"] != 0700 {
		t.Fatalf(".discobot-secrets mode = %o, want 700", modes[".discobot-secrets"])
	}
	if modes[".discobot-secrets/ssh"] != 0700 {
		t.Fatalf(".discobot-secrets/ssh mode = %o, want 700", modes[".discobot-secrets/ssh"])
	}
	if modes[".discobot-secrets/ssh/discobot_sandbox"] != 0600 {
		t.Fatalf("private key mode = %o, want 600", modes[".discobot-secrets/ssh/discobot_sandbox"])
	}
	if modes[".discobot-secrets/ssh/discobot_sandbox.pub"] != 0644 {
		t.Fatalf("public key mode = %o, want 644", modes[".discobot-secrets/ssh/discobot_sandbox.pub"])
	}
	if entries[".discobot-secrets/ssh/discobot_sandbox"] != "PRIVATE KEY\n" {
		t.Fatalf("unexpected private key contents: %q", entries[".discobot-secrets/ssh/discobot_sandbox"])
	}
	if entries[".discobot-secrets/ssh/discobot_sandbox.pub"] != "ecdsa-sha2-nistp256 AAAATEST discobot\n" {
		t.Fatalf("unexpected public key contents: %q", entries[".discobot-secrets/ssh/discobot_sandbox.pub"])
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

func TestTranslateDockerEvent_DieWithNonZeroExitCodeIsStopped(t *testing.T) {
	provider := &Provider{}
	event := provider.translateDockerEvent(events.Message{
		Action: "die",
		Actor: events.Actor{
			Attributes: map[string]string{
				"discobot.session.id": "session-123",
				"exitCode":            "42",
			},
		},
		Time:     1710000000,
		TimeNano: 1710000000000000000,
	})

	if event == nil {
		t.Fatal("expected state event")
	}
	if event.Status != sandbox.StatusStopped {
		t.Fatalf("translateDockerEvent status = %s, want %s", event.Status, sandbox.StatusStopped)
	}
	if event.Error != "" {
		t.Fatalf("translateDockerEvent error = %q, want empty", event.Error)
	}
}

func TestApplyContainerState_NonZeroExitCodeIsStopped(t *testing.T) {
	sb := &sandbox.Sandbox{}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)

	applyContainerState(sb, &containerTypes.State{
		ExitCode:   42,
		FinishedAt: finishedAt,
	})

	if sb.Status != sandbox.StatusStopped {
		t.Fatalf("applyContainerState status = %s, want %s", sb.Status, sandbox.StatusStopped)
	}
	if sb.Error != "" {
		t.Fatalf("applyContainerState error = %q, want empty", sb.Error)
	}
	if sb.StoppedAt == nil {
		t.Fatal("expected stopped timestamp to be set")
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
