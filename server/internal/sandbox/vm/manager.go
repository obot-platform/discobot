// Package vm provides an abstraction for project-level virtual machine management.
// Different implementations (VZ, KVM, WSL2) can provide VMs that run Docker daemon
// for container-based session isolation.
package vm

import (
	"context"
	"net"

	"github.com/obot-platform/discobot/server/internal/sandbox"
)

// ProjectVM represents a VM instance that hosts Docker daemon for multiple sessions.
type ProjectVM interface {
	// ProjectID returns the project ID this VM serves.
	ProjectID() string

	// DockerDialer returns a dialer function for connecting to Docker daemon inside the VM.
	// The dialer is used to create a Docker client with custom transport.
	DockerDialer() func(ctx context.Context, network, addr string) (net.Conn, error)

	// PortDialer returns a dialer function for connecting to an arbitrary VSOCK port.
	// This is used to reach forwarded ports (e.g., container published ports) inside the VM.
	PortDialer(port uint32) func(ctx context.Context, network, addr string) (net.Conn, error)

	// Shutdown gracefully stops the VM.
	Shutdown() error
}

// ProjectVMManager manages project-level VMs.
// Each implementation (VZ, KVM, WSL2) provides VMs that run Docker daemon,
// allowing multiple sessions within a project to share a VM while maintaining
// isolation via Docker containers.
type ProjectVMManager interface {
	// GetOrCreateVM returns an existing VM for the project or creates a new one.
	GetOrCreateVM(ctx context.Context, projectID string) (ProjectVM, error)

	// GetVM returns the VM for the given project, if it exists.
	GetVM(projectID string) (ProjectVM, bool)

	// ListProjectIDs returns the IDs of all projects that currently have an
	// active VM or persisted VM data that can be resumed.
	ListProjectIDs() []string

	// RemoveVM shuts down and removes the VM for the given project.
	// Returns nil if the project has no VM.
	RemoveVM(projectID string) error

	// Shutdown stops all VMs and cleans up resources.
	Shutdown()

	// Ready returns a channel that is closed when the manager is ready to create VMs.
	// Implementations that need async initialization (e.g., downloading images)
	// close this channel once initialization is complete.
	Ready() <-chan struct{}

	// Err returns any error that occurred during initialization.
	// Should only be called after Ready() is closed.
	Err() error
}

// StatusReporter is an optional interface that ProjectVMManager implementations
// can implement to provide detailed status information (e.g., download progress).
type StatusReporter interface {
	Status() sandbox.ProviderStatus
}

// ProviderResourceConfig contains effective per-project VM resource settings.
type ProviderResourceConfig struct {
	CPUCount   int
	MemoryMB   int
	DataDiskGB int
}

// ProviderResourceResolver resolves effective resource settings for a project.
type ProviderResourceResolver func(ctx context.Context, projectID string) (ProviderResourceConfig, error)

// ProviderResourceManager is an optional VM manager capability for resolving
// effective provider VM resources, including provider defaults.
type ProviderResourceManager interface {
	ProviderResources(ctx context.Context, projectID string) (ProviderResourceConfig, error)
}

// DiskResizer is an optional VM manager capability for growing project data disks.
type DiskResizer interface {
	ResizeDataDisk(ctx context.Context, projectID string, sizeGB int) error
}

// Config contains common configuration for VM managers.
type Config struct {
	// DataDir is where VM disk images and state are stored.
	DataDir string

	// ConsoleLogDir is where VM console logs are written.
	// Each project VM writes to {ConsoleLogDir}/project-{projectID}/console.log
	// Example: "~/.local/state/discobot/vz" for XDG compliance
	ConsoleLogDir string

	// KernelPath is the path to the Linux kernel (for VZ, KVM).
	KernelPath string

	// InitrdPath is the path to the initial ramdisk (optional).
	InitrdPath string

	// BaseDiskPath is the path to the base disk image to clone.
	// The base image should have Docker daemon pre-installed.
	BaseDiskPath string

	// ImageRef is the Docker registry image reference for auto-downloading
	// kernel and base disk if KernelPath and BaseDiskPath are not set.
	// Example: "ghcr.io/obot-platform/discobot-vz:main"
	ImageRef string

	// LauncherPath is the path to an external VM launcher binary, when the
	// provider starts VMs through a helper process.
	LauncherPath string

	// IdleTimeout is how long to wait before shutting down idle VMs.
	// Zero means VMs are never shut down automatically.
	IdleTimeout string

	// CPUCount is the number of CPUs per VM (0 = default).
	CPUCount int

	// MemoryMB is the memory per VM in megabytes (0 = default).
	MemoryMB int

	// DataDiskGB is the size of the writable data disk per VM in gigabytes (0 = default).
	DataDiskGB int

	// HomeDir is the host directory to share with the VM via VirtioFS (read-only).
	// If set, the directory is mounted at /host-home inside the guest.
	HomeDir string
}
