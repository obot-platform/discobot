// Package main is the entry point for the discobot-agent init process.
// This binary provides two subcommands:
// - setup: Container initialization (workspace, overlayfs, certs, env files)
// - proxy: VSOCK port proxy for VZ VMs (Docker event watching + socat forwarding)
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed default-proxy-config.yaml
var defaultProxyConfig []byte

const (
	// Default user to run as
	defaultUser = "discobot"

	// Proxy binary path
	proxyBinary = "/opt/discobot/bin/proxy"

	// Proxy port
	proxyPort = 17080

	// Paths
	dataDir      = "/.data"
	baseHomeDir  = "/.data/discobot"           // Base home directory (copied from /home/discobot)
	workspaceDir = "/.data/discobot/workspace" // Workspace inside home
	stagingDir   = "/.data/discobot/workspace.staging"
	overlayFSDir = "/.data/.overlayfs"
	mountHome    = "/home/discobot" // Where overlayfs mounts
	symlinkPath  = "/workspace"     // Symlink to /home/discobot/workspace
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: discobot-agent <setup|proxy>\n")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "setup":
		err = runSetup()
	case "proxy":
		err = runProxy()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nusage: discobot-agent <setup|proxy>\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "discobot-agent %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

// runSetup performs container initialization as a oneshot systemd service.
// It does all setup steps (workspace, overlayfs, certs, etc.) then writes
// environment files for other systemd services and exits.
func runSetup() error {
	startupStart := time.Now()
	fmt.Printf("discobot-agent: setup beginning at %s\n", startupStart.Format(time.RFC3339))

	// Change to root directory to avoid issues with overlayfs mounting
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir to /: %w", err)
	}

	// Step 0: Fix localhost resolution
	if err := fixLocalhostResolution(); err != nil {
		fmt.Printf("discobot-agent: warning: failed to fix localhost resolution: %v\n", err)
	}

	// Fix MTU for nested Docker
	if err := fixMTUForNestedDocker(); err != nil {
		fmt.Printf("discobot-agent: warning: failed to fix MTU for nested Docker: %v\n", err)
	}

	// Determine configuration from environment
	runAsUser := envOrDefault("AGENT_USER", defaultUser)
	sessionID := os.Getenv("SESSION_ID")
	workspacePath := os.Getenv("WORKSPACE_ORIGIN_PATH")
	workspaceCommit := os.Getenv("WORKSPACE_COMMIT")

	if sessionID == "" {
		return fmt.Errorf("SESSION_ID environment variable is required")
	}

	userInfo, err := lookupUser(runAsUser)
	if err != nil {
		return fmt.Errorf("failed to lookup user %s: %w", runAsUser, err)
	}

	// Step 0: Setup git safe.directory
	stepStart := time.Now()
	if err := setupGitSafeDirectories(workspacePath); err != nil {
		return fmt.Errorf("git safe.directory setup failed: %w", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] git safe.directory setup completed\n", time.Since(stepStart).Seconds())

	// Step 1: Setup base home directory
	stepStart = time.Now()
	if err := setupBaseHome(userInfo); err != nil {
		return fmt.Errorf("base home setup failed: %w", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] base home setup completed\n", time.Since(stepStart).Seconds())

	// Step 2: Clone workspace
	stepStart = time.Now()
	if err := setupWorkspace(workspacePath, workspaceCommit, userInfo); err != nil {
		return fmt.Errorf("workspace setup failed: %w", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] workspace setup completed\n", time.Since(stepStart).Seconds())

	// Step 3: Setup and mount OverlayFS for copy-on-write session isolation
	stepStart = time.Now()
	fmt.Printf("discobot-agent: using OverlayFS\n")

	if err := setupOverlayFS(sessionID, userInfo); err != nil {
		return fmt.Errorf("overlayfs setup failed: %w", err)
	}
	if err := mountOverlayFS(sessionID); err != nil {
		return fmt.Errorf("overlayfs mount failed: %w", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] filesystem setup completed (overlayfs)\n", time.Since(stepStart).Seconds())

	// Step 4: Mount cache directories
	stepStart = time.Now()
	if err := mountCacheDirectories(); err != nil {
		fmt.Printf("discobot-agent: Cache mount failed: %v\n", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] cache directories mounted\n", time.Since(stepStart).Seconds())

	// Step 5: Create /workspace symlink
	stepStart = time.Now()
	if err := createWorkspaceSymlink(); err != nil {
		return fmt.Errorf("symlink creation failed: %w", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] workspace symlink created\n", time.Since(stepStart).Seconds())

	// Step 5.5: Run session hooks
	// In oneshot mode we must wait for background hooks before the process exits.
	stepStart = time.Now()
	waitHooks := runSessionHooks(filepath.Join(mountHome, "workspace"), userInfo)
	fmt.Printf("discobot-agent: [%.3fs] session hooks dispatched\n", time.Since(stepStart).Seconds())

	// Step 6: Setup proxy configuration
	stepStart = time.Now()
	if err := setupProxyConfig(userInfo); err != nil {
		fmt.Printf("discobot-agent: Proxy config setup failed: %v\n", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] proxy config setup completed\n", time.Since(stepStart).Seconds())

	// Step 7: Generate CA certificate
	stepStart = time.Now()
	if err := setupProxyCertificate(); err != nil {
		fmt.Printf("discobot-agent: Proxy certificate setup failed: %v\n", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] CA certificate setup completed\n", time.Since(stepStart).Seconds())

	// Step 8: Write Docker daemon configuration
	stepStart = time.Now()
	if err := writeDockerDaemonConfig(); err != nil {
		fmt.Printf("discobot-agent: Docker daemon config failed: %v\n", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] Docker daemon config written\n", time.Since(stepStart).Seconds())

	// Step 8.5: Remove stale buildx default-builder config so that no session
	// inherits a pointer to a remote BuildKit instance from a previous run.
	buildxDir := filepath.Join(userInfo.homeDir, ".docker", "buildx")
	for _, stale := range []string{
		filepath.Join(buildxDir, "current"),
		filepath.Join(buildxDir, "instances", "discobot-shared"),
	} {
		if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
			fmt.Printf("discobot-agent: warning: failed to remove stale buildx config %s: %v\n", stale, err)
		}
	}

	// Step 9: Write environment files for systemd services
	stepStart = time.Now()
	if err := writeProxyEnvironmentFile(); err != nil {
		fmt.Printf("discobot-agent: warning: failed to write proxy env file: %v\n", err)
	}
	if err := writeAgentEnvironmentFile(userInfo); err != nil {
		return fmt.Errorf("failed to write agent environment file: %w", err)
	}
	fmt.Printf("discobot-agent: [%.3fs] environment files written\n", time.Since(stepStart).Seconds())

	// Notify systemd that setup is complete so dependent services can start
	// while background session hooks continue running.
	fmt.Printf("discobot-agent: [%.3fs] setup completed successfully\n", time.Since(startupStart).Seconds())
	if err := sdNotifyReady(); err != nil {
		fmt.Printf("discobot-agent: warning: sd_notify failed: %v\n", err)
	}

	// Wait for any background session hooks to finish before exiting,
	// otherwise the process exit will kill in-flight hooks.
	stepStart = time.Now()
	waitHooks()
	if d := time.Since(stepStart); d > 50*time.Millisecond {
		fmt.Printf("discobot-agent: [%.3fs] waited for background session hooks\n", d.Seconds())
	}

	return nil
}

// sdNotifyReady sends READY=1 to the systemd notification socket, signalling
// that the service has finished its critical startup. This unblocks dependent
// units (e.g. discobot-proxy, discobot-agent-api) while the process continues
// running to drain background work like session hooks.
// If NOTIFY_SOCKET is not set (e.g. running outside systemd), this is a no-op.
func sdNotifyReady() error {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return nil
	}

	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		return fmt.Errorf("dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("READY=1")); err != nil {
		return fmt.Errorf("write READY=1: %w", err)
	}
	return nil
}

// writeDockerDaemonConfig creates /etc/docker/daemon.json with MTU derived from
// the current interface. This is called during setup so dockerd can start with
// the correct configuration.
func writeDockerDaemonConfig() error {
	// Check if dockerd is on PATH
	if _, err := exec.LookPath("dockerd"); err != nil {
		return fmt.Errorf("dockerd not found on PATH: %w", err)
	}

	// Ensure directories exist
	if err := os.MkdirAll("/etc/docker", 0755); err != nil {
		return fmt.Errorf("failed to create /etc/docker: %w", err)
	}

	dockerDataDir := filepath.Join(dataDir, "docker")
	if err := os.MkdirAll(dockerDataDir, 0755); err != nil {
		return fmt.Errorf("failed to create docker data dir: %w", err)
	}

	// Read current MTU from eth0
	mtuBytes, err := os.ReadFile("/sys/class/net/eth0/mtu")
	if err != nil {
		return fmt.Errorf("failed to read current MTU: %w", err)
	}
	currentMTU, err := strconv.Atoi(strings.TrimSpace(string(mtuBytes)))
	if err != nil {
		return fmt.Errorf("failed to parse MTU: %w", err)
	}

	// Subtract overhead for Docker networking
	dockerMTU := currentMTU - 100
	if dockerMTU < 1200 {
		dockerMTU = 1200
	}

	daemonConfig := map[string]interface{}{
		"mtu": dockerMTU,
		"features": map[string]interface{}{
			"containerd-snapshotter": true,
		},
	}
	configBytes, err := json.MarshalIndent(daemonConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal daemon config: %w", err)
	}
	if err := os.WriteFile("/etc/docker/daemon.json", configBytes, 0644); err != nil {
		return fmt.Errorf("failed to write daemon.json: %w", err)
	}
	fmt.Printf("discobot-agent: configured Docker daemon with MTU=%d (interface MTU: %d, overhead: 100)\n", dockerMTU, currentMTU)

	return nil
}

// writeProxyEnvironmentFile writes proxy environment variables to /run/discobot/proxy-env
// so the dockerd service can use them for image pulls through the proxy.
func writeProxyEnvironmentFile() error {
	// Check if proxy binary exists
	if _, err := os.Stat(proxyBinary); err != nil {
		return fmt.Errorf("proxy binary not found: %w", err)
	}

	proxyEnvPath := "/run/discobot/proxy-env"
	if err := os.MkdirAll(filepath.Dir(proxyEnvPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var lines []string
	for _, envVar := range getProxyEnvVars() {
		lines = append(lines, envVar)
	}

	if err := os.WriteFile(proxyEnvPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write proxy env file: %w", err)
	}

	fmt.Printf("discobot-agent: proxy environment written to %s\n", proxyEnvPath)

	// Also set proxy in /etc/profile.d for login shells
	if err := setProxyInProfile(); err != nil {
		fmt.Printf("discobot-agent: warning: failed to set proxy in /etc/profile.d: %v\n", err)
	}

	return nil
}

// writeAgentEnvironmentFile writes the environment file used by the
// discobot-agent-api systemd service at /run/discobot/agent-env.
func writeAgentEnvironmentFile(u *userInfo) error {
	envPath := "/run/discobot/agent-env"
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Build the environment for the agent-api service
	env := buildChildEnv(u, true)

	var lines []string
	for _, e := range env {
		lines = append(lines, e)
	}

	if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write agent env file: %w", err)
	}

	fmt.Printf("discobot-agent: agent environment written to %s\n", envPath)
	return nil
}

// fixLocalhostResolution modifies /etc/hosts to ensure localhost resolves to IPv4 (127.0.0.1).
// This fixes IPv4/IPv6 mismatches where Node.js servers bind to ::1 (IPv6) by default when
// using "localhost", but HTTP clients (like Bun's fetch) resolve localhost to 127.0.0.1 (IPv4).
// The fix removes ::1 from the localhost line to force consistent IPv4 resolution.
func fixLocalhostResolution() error {
	const hostsPath = "/etc/hosts"

	// Read current hosts file
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", hostsPath, err)
	}

	lines := strings.Split(string(data), "\n")
	var newLines []string
	modified := false
	hasIPv4Localhost := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			newLines = append(newLines, line)
			continue
		}

		// Parse the line: first field is IP, rest are hostnames
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			newLines = append(newLines, line)
			continue
		}

		ip := fields[0]
		hostnames := fields[1:]

		// Check if this line has "localhost" as a hostname
		hasLocalhost := false
		for _, h := range hostnames {
			if h == "localhost" {
				hasLocalhost = true
				break
			}
		}

		if !hasLocalhost {
			// Line doesn't affect localhost resolution, keep it
			newLines = append(newLines, line)
			continue
		}

		// This line has localhost
		switch ip {
		case "127.0.0.1":
			// Keep IPv4 localhost line
			newLines = append(newLines, line)
			hasIPv4Localhost = true
		case "::1":
			// Remove localhost from IPv6 line, but keep other hostnames
			var remainingHostnames []string
			for _, h := range hostnames {
				if h != "localhost" {
					remainingHostnames = append(remainingHostnames, h)
				}
			}

			if len(remainingHostnames) > 0 {
				// Keep the line with remaining hostnames (e.g., ip6-localhost)
				newLines = append(newLines, ip+"\t"+strings.Join(remainingHostnames, " "))
			}
			// If no remaining hostnames, the line is dropped entirely
			modified = true
			fmt.Printf("discobot-agent: removed 'localhost' from ::1 line in /etc/hosts\n")
		default:
			// Some other IP with localhost, keep it
			newLines = append(newLines, line)
		}
	}

	// Ensure we have an IPv4 localhost entry
	if !hasIPv4Localhost {
		newLines = append([]string{"127.0.0.1\tlocalhost"}, newLines...)
		modified = true
		fmt.Printf("discobot-agent: added '127.0.0.1 localhost' to /etc/hosts\n")
	}

	if !modified {
		fmt.Printf("discobot-agent: /etc/hosts already configured correctly for localhost\n")
		return nil
	}

	// Write back the modified hosts file
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(hostsPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostsPath, err)
	}

	fmt.Printf("discobot-agent: /etc/hosts updated to ensure localhost resolves to 127.0.0.1\n")
	return nil
}

// fixMTUForNestedDocker configures TCP settings to work around MTU blackhole issues
// in nested Docker environments where path MTU discovery fails.
//
// The fix works by:
// 1. Disabling PMTU discovery (which relies on ICMP that gets blocked in nested Docker)
// 2. Enabling TCP MTU probing (ICMP-free mechanism that auto-detects working packet size)
//
// With these settings, TCP automatically discovers the optimal MTU without needing to
// reduce the interface MTU, allowing maximum throughput while avoiding packet drops.
func fixMTUForNestedDocker() error {
	// Disable path MTU discovery to prevent relying on ICMP (which may be blocked in nested Docker)
	// When PMTUD fails, packets are sent at full MTU and silently dropped if too large
	cmd := exec.Command("sysctl", "-w", "net.ipv4.ip_no_pmtu_disc=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to disable PMTU discovery: %w (output: %s)", err, output)
	}

	// Enable TCP MTU probing as a fallback mechanism
	// This allows TCP to discover working MTU by detecting dropped packets and trying smaller sizes
	// This works without ICMP and is essential for nested Docker where ICMP is unreliable
	cmd = exec.Command("sysctl", "-w", "net.ipv4.tcp_mtu_probing=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable TCP MTU probing: %w (output: %s)", err, output)
	}

	fmt.Printf("discobot-agent: configured TCP MTU probing for nested Docker (PMTUD disabled, TCP probing enabled)\n")
	return nil
}

// setupGitSafeDirectories configures git safe.directory for all workspace paths.
// Uses --system to write to /etc/gitconfig so all users (including discobot) can see it.
func setupGitSafeDirectories(workspacePath string) error {
	// Paths that need to be marked as safe for git operations
	dirs := []string{
		"/.workspace",                         // Source workspace mount point
		"/.workspace/.git",                    // Git directory (some operations check .git specifically)
		workspaceDir,                          // /.data/discobot/workspace
		stagingDir,                            // /.data/discobot/workspace.staging (used during clone)
		filepath.Join(mountHome, "workspace"), // /home/discobot/workspace (after overlayfs mount)
		symlinkPath,                           // /workspace symlink
	}

	// Add the specific workspacePath if provided and different from /.workspace
	if workspacePath != "" && workspacePath != "/.workspace" {
		dirs = append([]string{workspacePath}, dirs...)
	}

	fmt.Printf("discobot-agent: configuring git safe.directory for workspace paths\n")
	for _, dir := range dirs {
		cmd := exec.Command("git", "config", "--system", "--add", "safe.directory", dir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// Log but don't fail - some paths may not exist yet
			fmt.Printf("discobot-agent: warning: git config safe.directory %s: %v\n", dir, err)
		}
	}

	return nil
}

// setupBaseHome copies /home/discobot to /.data/discobot if it doesn't exist,
// or syncs new files if it already exists
func setupBaseHome(u *userInfo) error {
	// Check if base home already exists
	if _, err := os.Stat(baseHomeDir); err == nil {
		fmt.Printf("discobot-agent: base home already exists at %s, syncing new files\n", baseHomeDir)
		// Sync any new files from /home/discobot to /.data/discobot
		// This ensures new files added to the container image get propagated
		if err := syncNewFiles(mountHome, baseHomeDir, u); err != nil {
			return fmt.Errorf("failed to sync new files: %w", err)
		}
		return nil
	}

	fmt.Printf("discobot-agent: copying /home/discobot to %s\n", baseHomeDir)

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(baseHomeDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Copy /home/discobot to /.data/discobot recursively with permissions
	if err := copyDir(mountHome, baseHomeDir); err != nil {
		return fmt.Errorf("failed to copy home directory: %w", err)
	}

	// Ensure ownership is correct
	if err := chownRecursive(baseHomeDir, u.uid, u.gid); err != nil {
		return fmt.Errorf("failed to chown base home: %w", err)
	}

	fmt.Printf("discobot-agent: base home created successfully\n")
	return nil
}

// syncNewFiles copies files from src to dst that don't exist in dst.
// It does not overwrite existing files to preserve user modifications.
func syncNewFiles(src, dst string, u *userInfo) error {
	return filepath.Walk(src, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path and destination path
		relPath, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		// Check if destination already exists
		_, dstErr := os.Lstat(dstPath)
		if dstErr == nil {
			// Destination exists, skip (don't overwrite)
			return nil
		}
		if !os.IsNotExist(dstErr) {
			// Some other error
			return dstErr
		}

		// Destination doesn't exist, copy it
		if info.IsDir() {
			fmt.Printf("discobot-agent: syncing new directory %s\n", relPath)
			if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
				return err
			}
			if err := os.Chown(dstPath, u.uid, u.gid); err != nil {
				return err
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			// Handle symlinks
			link, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			fmt.Printf("discobot-agent: syncing new symlink %s\n", relPath)
			if err := os.Symlink(link, dstPath); err != nil {
				return err
			}
			if err := os.Lchown(dstPath, u.uid, u.gid); err != nil {
				return err
			}
		} else if info.Mode().IsRegular() {
			fmt.Printf("discobot-agent: syncing new file %s\n", relPath)
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
			if err := os.Chown(dstPath, u.uid, u.gid); err != nil {
				return err
			}
		}

		return nil
	})
}

// copyDir recursively copies a directory preserving permissions
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory with same permissions
	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else if entry.Type()&os.ModeSymlink != 0 {
			// Handle symlinks
			link, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(link, dstPath); err != nil {
				return err
			}
		} else {
			// Copy regular file
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file preserving permissions
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "discobot-agent: warning: failed to close source file %s: %v\n", src, closeErr)
		}
	}()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dstFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "discobot-agent: warning: failed to close destination file %s: %v\n", dst, closeErr)
		}
	}()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// setupWorkspace clones the workspace if it doesn't exist.
func setupWorkspace(workspacePath, workspaceCommit string, u *userInfo) error {
	// If workspace already exists, nothing to do
	if _, err := os.Stat(workspaceDir); err == nil {
		fmt.Printf("discobot-agent: workspace already exists at %s\n", workspaceDir)
		return nil
	}

	// If no workspace path specified, create empty workspace owned by user
	if workspacePath == "" {
		fmt.Println("discobot-agent: no WORKSPACE_ORIGIN_PATH specified, creating empty workspace")
		if err := os.MkdirAll(workspaceDir, 0755); err != nil {
			return fmt.Errorf("failed to create workspace directory: %w", err)
		}
		if err := os.Chown(workspaceDir, u.uid, u.gid); err != nil {
			return fmt.Errorf("failed to chown workspace directory: %w", err)
		}
		return nil
	}

	fmt.Printf("discobot-agent: cloning workspace from %s\n", workspacePath)

	// Clean up any existing staging directory
	if err := os.RemoveAll(stagingDir); err != nil {
		return fmt.Errorf("failed to remove staging directory: %w", err)
	}

	// Note: git safe.directory is configured system-wide in setupGitSafeDirectories()

	// Clone to staging directory first
	cloneArgs := []string{"clone", "--single-branch", workspacePath, stagingDir}

	cmd := exec.Command("git", cloneArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("discobot-agent: running: git %v\n", cloneArgs)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	// If specific commit requested, create a branch at that commit to avoid detached HEAD
	if workspaceCommit != "" {
		// Create a temporary branch at the target commit
		branchName := "discobot-session"
		cmd = exec.Command("git", "-C", stagingDir, "checkout", "-B", branchName, workspaceCommit)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("discobot-agent: creating branch %s at commit %s\n", branchName, workspaceCommit)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git checkout -B %s %s failed: %w", branchName, workspaceCommit, err)
		}
	}

	// Change ownership of all files to the target user
	fmt.Printf("discobot-agent: changing workspace ownership to %s\n", u.username)
	if err := chownRecursive(stagingDir, u.uid, u.gid); err != nil {
		return fmt.Errorf("failed to chown workspace: %w", err)
	}

	// Atomically move staging to final location
	if err := os.Rename(stagingDir, workspaceDir); err != nil {
		return fmt.Errorf("failed to move staging to workspace: %w", err)
	}

	fmt.Printf("discobot-agent: workspace cloned successfully\n")
	return nil
}

// chownRecursive recursively changes ownership of a directory and all its contents
func chownRecursive(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(name, uid, gid)
	})
}

// setupOverlayFS creates the directory structure for overlayfs
func setupOverlayFS(sessionID string, u *userInfo) error {
	sessionDir := filepath.Join(overlayFSDir, sessionID)
	upperDir := filepath.Join(sessionDir, "upper")
	workDir := filepath.Join(sessionDir, "work")

	fmt.Printf("discobot-agent: setting up overlayfs directories at %s\n", sessionDir)

	// Create all directories
	for _, dir := range []string{overlayFSDir, sessionDir, upperDir, workDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set ownership on session-specific directories
	for _, dir := range []string{sessionDir, upperDir, workDir} {
		if err := os.Chown(dir, u.uid, u.gid); err != nil {
			return fmt.Errorf("failed to chown directory %s: %w", dir, err)
		}
	}

	fmt.Printf("discobot-agent: overlayfs directories created successfully\n")
	return nil
}

// mountOverlayFS mounts the overlayfs filesystem over /home/discobot
func mountOverlayFS(sessionID string) error {
	sessionDir := filepath.Join(overlayFSDir, sessionID)
	upperDir := filepath.Join(sessionDir, "upper")
	workDir := filepath.Join(sessionDir, "work")

	// Construct mount options:
	// lowerdir = read-only base layer
	// upperdir = writable layer for changes
	// workdir = scratch space for overlayfs internal use
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", baseHomeDir, upperDir, workDir)

	fmt.Printf("discobot-agent: mounting overlayfs at %s\n", mountHome)
	fmt.Printf("discobot-agent: overlayfs options: %s\n", opts)

	if err := syscall.Mount("overlay", mountHome, "overlay", 0, opts); err != nil {
		return fmt.Errorf("overlayfs mount failed: %w", err)
	}

	fmt.Printf("discobot-agent: overlayfs mounted successfully\n")
	return nil
}

// createWorkspaceSymlink creates /workspace -> /home/discobot/workspace symlink
func createWorkspaceSymlink() error {
	target := filepath.Join(mountHome, "workspace")

	// Remove existing symlink or file if present
	if _, err := os.Lstat(symlinkPath); err == nil {
		if err := os.Remove(symlinkPath); err != nil {
			return fmt.Errorf("failed to remove existing %s: %w", symlinkPath, err)
		}
	}

	fmt.Printf("discobot-agent: creating symlink %s -> %s\n", symlinkPath, target)
	if err := os.Symlink(target, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// getProxyEnvVars returns the proxy environment variables if proxy is enabled.
func getProxyEnvVars() []string {
	proxyURL := fmt.Sprintf("http://localhost:%d", proxyPort)
	noProxy := "localhost,127.0.0.1,::1"
	caCertPath := filepath.Join(dataDir, "proxy", "certs", "ca.crt")
	return []string{
		"HTTP_PROXY=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"https_proxy=" + proxyURL,
		"ALL_PROXY=" + proxyURL,
		"all_proxy=" + proxyURL,
		"NO_PROXY=" + noProxy,
		"no_proxy=" + noProxy,
		"NODE_EXTRA_CA_CERTS=" + caCertPath,
	}
}

// setProxyInProfile writes proxy environment variables to /etc/profile.d/discobot-proxy.sh
// so that login shells automatically inherit the proxy configuration.
func setProxyInProfile() error {
	profileDir := "/etc/profile.d"

	// Check if /etc/profile.d exists
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		// If /etc/profile.d doesn't exist, try /etc/profile directly
		return setProxyInEtcProfile()
	}

	// Write proxy settings to /etc/profile.d/discobot-proxy.sh
	profilePath := filepath.Join(profileDir, "discobot-proxy.sh")
	proxyURL := fmt.Sprintf("http://localhost:%d", proxyPort)
	caCertPath := filepath.Join(dataDir, "proxy", "certs", "ca.crt")

	content := fmt.Sprintf(`# Discobot Proxy Configuration
# Automatically generated by discobot-agent
# This file sets proxy environment variables for all login shells

export HTTP_PROXY=%s
export HTTPS_PROXY=%s
export http_proxy=%s
export https_proxy=%s
export ALL_PROXY=%s
export all_proxy=%s

# Bypass proxy for localhost
export NO_PROXY=localhost,127.0.0.1,::1
export no_proxy=localhost,127.0.0.1,::1

# Node.js: Trust the proxy's CA certificate
export NODE_EXTRA_CA_CERTS=%s
`, proxyURL, proxyURL, proxyURL, proxyURL, proxyURL, proxyURL, caCertPath)

	if err := os.WriteFile(profilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", profilePath, err)
	}

	fmt.Printf("discobot-agent: proxy settings written to %s\n", profilePath)
	return nil
}

// setProxyInEtcProfile appends proxy settings to /etc/profile if /etc/profile.d doesn't exist.
func setProxyInEtcProfile() error {
	profilePath := "/etc/profile"

	// Check if /etc/profile exists
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		return fmt.Errorf("neither /etc/profile.d nor /etc/profile exists")
	}

	proxyURL := fmt.Sprintf("http://localhost:%d", proxyPort)
	caCertPath := filepath.Join(dataDir, "proxy", "certs", "ca.crt")

	content := fmt.Sprintf(`

# Discobot Proxy Configuration (added by discobot-agent)
export HTTP_PROXY=%s
export HTTPS_PROXY=%s
export http_proxy=%s
export https_proxy=%s
export ALL_PROXY=%s
export all_proxy=%s
export NO_PROXY=localhost,127.0.0.1,::1
export no_proxy=localhost,127.0.0.1,::1
export NODE_EXTRA_CA_CERTS=%s
`, proxyURL, proxyURL, proxyURL, proxyURL, proxyURL, proxyURL, caCertPath)

	// Append to /etc/profile
	f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", profilePath, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to %s: %w", profilePath, err)
	}

	fmt.Printf("discobot-agent: proxy settings appended to %s\n", profilePath)
	return nil
}

// setupProxyCertificate generates a CA certificate for the proxy and installs it in the system trust store.
// The certificate is stored in /.data/proxy/certs/ (session-scoped) and will be used by the proxy for HTTPS MITM.
func setupProxyCertificate() error {
	certDir := filepath.Join(dataDir, "proxy", "certs")
	certPath := filepath.Join(certDir, "ca.crt")
	keyPath := filepath.Join(certDir, "ca.key")

	// Ensure cert directory exists
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("failed to create cert dir: %w", err)
	}

	// Check if certificate already exists
	if _, err := os.Stat(certPath); err == nil {
		fmt.Printf("discobot-agent: proxy CA certificate already exists at %s\n", certPath)
		// Certificate exists, ensure it's installed in system trust store
		return installCertificateInSystemTrust(certPath)
	}

	fmt.Printf("discobot-agent: generating proxy CA certificate...\n")

	// Generate CA certificate using the proxy's cert package
	// We'll call the proxy binary with a special flag to generate the cert
	// Since we don't want to import the proxy code, we'll generate it inline
	if err := generateCACertificate(certPath, keyPath); err != nil {
		return fmt.Errorf("failed to generate CA certificate: %w", err)
	}

	fmt.Printf("discobot-agent: proxy CA certificate generated at %s\n", certPath)

	// Install certificate in system trust store
	return installCertificateInSystemTrust(certPath)
}

// generateCACertificate creates a CA certificate and private key using Go crypto libraries.
// Includes localhost in SANs for proper HTTPS interception.
func generateCACertificate(certPath, keyPath string) error {
	// Generate RSA private key (2048-bit)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate RSA key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial number: %w", err)
	}

	// Create certificate template
	// Include localhost in SANs for proper HTTPS interception
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Discobot Proxy"},
			CommonName:   "Discobot Proxy CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		// Add SANs for localhost (both IPv4 and IPv6)
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	// Save certificate (PEM format)
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	defer func() { _ = certFile.Close() }()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("encode certificate: %w", err)
	}

	// Save private key (PEM format)
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	defer func() { _ = keyFile.Close() }()

	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}); err != nil {
		return fmt.Errorf("encode private key: %w", err)
	}

	return nil
}

// installCertificateInSystemTrust installs the CA certificate in the system trust store.
// Supports Debian/Ubuntu, Fedora/RHEL, and Alpine Linux.
func installCertificateInSystemTrust(certPath string) error {
	fmt.Printf("discobot-agent: installing proxy CA certificate in system trust store...\n")

	// Detect which certificate update method to use
	// Try in order: update-ca-certificates (Debian/Alpine), update-ca-trust (Fedora)

	// Debian/Ubuntu/Alpine: update-ca-certificates
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		return installCertDebianStyle(certPath)
	}

	// Fedora/RHEL/CentOS: update-ca-trust
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		return installCertFedoraStyle(certPath)
	}

	// If no cert update tool found, warn but don't fail
	fmt.Printf("discobot-agent: warning: no certificate update tool found (update-ca-certificates or update-ca-trust)\n")
	fmt.Printf("discobot-agent: warning: proxy CA certificate not installed in system trust store\n")
	fmt.Printf("discobot-agent: warning: HTTPS interception may not work for some clients\n")
	return nil
}

// installCertDebianStyle installs the certificate on Debian/Ubuntu/Alpine systems.
func installCertDebianStyle(certPath string) error {
	// Copy certificate to /usr/local/share/ca-certificates/
	destDir := "/usr/local/share/ca-certificates"
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create ca-certificates dir: %w", err)
	}

	destPath := filepath.Join(destDir, "discobot-proxy-ca.crt")

	// Read source certificate
	data, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}

	// Write to destination
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write certificate to %s: %w", destPath, err)
	}

	// Run update-ca-certificates
	cmd := exec.Command("update-ca-certificates")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run update-ca-certificates: %w", err)
	}

	fmt.Printf("discobot-agent: proxy CA certificate installed in system trust store (Debian/Ubuntu/Alpine)\n")
	return nil
}

// installCertFedoraStyle installs the certificate on Fedora/RHEL/CentOS systems.
func installCertFedoraStyle(certPath string) error {
	// Copy certificate to /etc/pki/ca-trust/source/anchors/
	destDir := "/etc/pki/ca-trust/source/anchors"
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create ca-trust dir: %w", err)
	}

	destPath := filepath.Join(destDir, "discobot-proxy-ca.crt")

	// Read source certificate
	data, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}

	// Write to destination
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write certificate to %s: %w", destPath, err)
	}

	// Run update-ca-trust
	cmd := exec.Command("update-ca-trust", "extract")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run update-ca-trust: %w", err)
	}

	fmt.Printf("discobot-agent: proxy CA certificate installed in system trust store (Fedora/RHEL)\n")
	return nil
}

// setupProxyConfig configures the proxy using embedded defaults only.
// Note: Reading workspace config would be a security risk since untrusted code could be executed
// before the sandbox is fully set up. The proxy always uses safe, built-in defaults.
func setupProxyConfig(userInfo *userInfo) error {
	proxyDataDir := filepath.Join(dataDir, "proxy")
	configDest := filepath.Join(proxyDataDir, "config.yaml")

	// Ensure proxy data directory exists
	if err := os.MkdirAll(proxyDataDir, 0755); err != nil {
		return fmt.Errorf("failed to create proxy data dir: %w", err)
	}

	// Always use built-in defaults (with Docker caching enabled)
	// Security: Never read workspace config during init as it's untrusted code
	fmt.Printf("discobot-agent: using default proxy config with Docker caching enabled\n")

	// Write config with restrictive permissions (0644) and keep as root-owned
	// This prevents the discobot user from modifying the proxy configuration
	if err := os.WriteFile(configDest, defaultProxyConfig, 0644); err != nil {
		return fmt.Errorf("failed to write default proxy config: %w", err)
	}

	// Config remains root-owned for security (no chown needed)

	return nil
}


// envOrDefault returns the environment variable value or the default if not set
func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// userInfo contains all information needed for user switching
type userInfo struct {
	uid      int
	gid      int
	username string
	homeDir  string
	groups   []uint32
}

// lookupUser returns user information for the given username
func lookupUser(username string) (*userInfo, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("invalid uid: %w", err)
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, fmt.Errorf("invalid gid: %w", err)
	}

	// Get supplementary groups
	groupIDs, err := u.GroupIds()
	if err != nil {
		// Non-fatal: continue without supplementary groups
		groupIDs = nil
	}

	groups := make([]uint32, 0, len(groupIDs))
	for _, gidStr := range groupIDs {
		g, err := strconv.Atoi(gidStr)
		if err == nil {
			groups = append(groups, uint32(g))
		}
	}

	return &userInfo{
		uid:      uid,
		gid:      gid,
		username: u.Username,
		homeDir:  u.HomeDir,
		groups:   groups,
	}, nil
}

// buildChildEnv creates the environment for the child process
// It inherits from parent but overrides user-specific variables
func buildChildEnv(u *userInfo, proxyEnabled bool) []string {
	// Start with parent environment
	parentEnv := os.Environ()
	env := make([]string, 0, len(parentEnv)+12) // +3 for user vars, +9 for proxy vars (including NO_PROXY and NODE_EXTRA_CA_CERTS)

	// Copy parent env, excluding user-specific vars we'll override
	skipVars := map[string]bool{
		"HOME":    true,
		"USER":    true,
		"LOGNAME": true,
	}

	for _, e := range parentEnv {
		// Extract variable name (everything before first '=')
		if varName, _, ok := strings.Cut(e, "="); ok && !skipVars[varName] {
			env = append(env, e)
		}
	}

	// Set user-specific environment variables
	env = append(env,
		"HOME="+u.homeDir,
		"USER="+u.username,
		"LOGNAME="+u.username,
	)

	// Enable hooks in the agent-api (only in container context)
	env = append(env, "DISCOBOT_HOOKS_ENABLED=true")

	// Add proxy environment variables if proxy is running
	if proxyEnabled {
		env = append(env, getProxyEnvVars()...)
	}

	return env
}

// ===== Cache Volume Mount Support =====

// cacheConfig defines the cache directory configuration.
type cacheConfig struct {
	AdditionalPaths []string `json:"additionalPaths,omitempty"`
}

// wellKnownCachePaths returns the list of well-known cache directories.
// Note: We only include .cache since all subdirectories under it will be cached.
func wellKnownCachePaths() []string {
	return []string{
		// Universal cache directory - all subdirectories will be cached
		"/home/discobot/.cache",

		// Package managers that don't use .cache
		"/home/discobot/.npm",
		"/home/discobot/.pnpm-store",
		"/home/discobot/.yarn",

		// Python
		"/home/discobot/.local/share/uv",

		// Go
		"/home/discobot/go/pkg/mod",

		// Rust / Cargo / Rustup
		"/home/discobot/.cargo/registry",
		"/home/discobot/.cargo/git",
		"/home/discobot/.rustup",

		// Ruby
		"/home/discobot/.bundle",
		"/home/discobot/.gem",

		// Java / Maven / Gradle
		"/home/discobot/.m2/repository",
		"/home/discobot/.gradle/caches",
		"/home/discobot/.gradle/wrapper",

		// .NET
		"/home/discobot/.nuget/packages",

		// PHP
		"/home/discobot/.composer/cache",

		// Nix store for single-user installs (writable for discobot via cache mount perms)
		"/nix",

		// System package cache (apt)
		"/var/cache/apt",

		// Build caches
		"/home/discobot/.ccache",

		// IDE caches
		"/home/discobot/.vscode-server",
		"/home/discobot/.cursor-server",
		"/home/discobot/.zed_server",
	}
}

// loadCacheConfig loads the cache configuration from the workspace.
// If the file doesn't exist or can't be read, returns default config.
func loadCacheConfig() *cacheConfig {
	configPath := filepath.Join(mountHome, "workspace", ".discobot", "cache.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// No config file is not an error - return empty config
		return &cacheConfig{}
	}

	var cfg cacheConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Printf("discobot-agent: warning: failed to parse cache config: %v\n", err)
		return &cacheConfig{}
	}

	return &cfg
}

// getAllCachePaths returns all cache paths (well-known + additional from config).
// Additional paths are validated to ensure they're within /home/discobot for security.
func getAllCachePaths(cfg *cacheConfig) []string {
	paths := make([]string, 0, len(wellKnownCachePaths())+len(cfg.AdditionalPaths))
	paths = append(paths, wellKnownCachePaths()...)

	// Validate and add additional paths
	for _, p := range cfg.AdditionalPaths {
		if isValidCachePath(p) {
			paths = append(paths, p)
		} else {
			fmt.Printf("discobot-agent: warning: ignoring invalid cache path from config: %s\n", p)
		}
	}

	return paths
}

// isValidCachePath checks if a path is safe to use as a cache directory.
// Only paths within /home/discobot are allowed for security.
func isValidCachePath(path string) bool {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(path)

	// Must be absolute path
	if !filepath.IsAbs(cleanPath) {
		return false
	}

	// Must be within /home/discobot (not equal to it, must be a subdirectory)
	homePrefix := "/home/discobot/"
	if !strings.HasPrefix(cleanPath+"/", homePrefix) {
		return false
	}

	// Must not contain any suspicious components
	// This prevents paths like /home/discobot/../etc
	if strings.Contains(cleanPath, "..") {
		return false
	}

	return true
}

// mountCacheDirectories bind-mounts cache directories from /.data/cache to their runtime paths.
// This is called after the overlay filesystem is mounted, so cache mounts sit on top of the overlay.
func mountCacheDirectories() error {
	// Check if CACHE_ENABLED environment variable is set
	if cacheEnabled := os.Getenv("CACHE_ENABLED"); cacheEnabled == "false" {
		fmt.Printf("discobot-agent: cache volumes disabled via CACHE_ENABLED=false\n")
		return nil
	}

	// Check if /.data/cache exists (created by Docker provider)
	cacheVolumeBase := filepath.Join(dataDir, "cache")
	if _, err := os.Stat(cacheVolumeBase); os.IsNotExist(err) {
		fmt.Printf("discobot-agent: cache volume not found at %s, skipping cache mounts\n", cacheVolumeBase)
		return nil
	}

	// Load cache configuration
	cfg := loadCacheConfig()

	// Get all cache paths
	cachePaths := getAllCachePaths(cfg)

	mounted := 0
	for _, cachePath := range cachePaths {
		// Clean the path to create a safe subdirectory name in the cache volume
		// e.g., "/home/discobot/.npm" -> "home/discobot/.npm"
		subDir := filepath.Clean(cachePath)
		if subDir[0] == '/' {
			subDir = subDir[1:]
		}

		// Source is in the cache volume
		source := filepath.Join(cacheVolumeBase, subDir)

		// Ensure the source directory exists in the cache volume with world-writable permissions
		// This allows all users/processes to write to cache directories
		if err := os.MkdirAll(source, 0777); err != nil {
			fmt.Printf("discobot-agent: warning: failed to create cache dir %s: %v\n", source, err)
			continue
		}
		// Explicitly set permissions to 0777 on the entire tree (umask may have restricted MkdirAll)
		chmodPathToRoot(source, cacheVolumeBase, 0777)

		// Ensure the target directory exists in the overlay with world-writable permissions
		if err := os.MkdirAll(cachePath, 0777); err != nil {
			fmt.Printf("discobot-agent: warning: failed to create target dir %s: %v\n", cachePath, err)
			continue
		}
		// Explicitly set permissions to 0777 on the entire tree (umask may have restricted MkdirAll).
		// For paths outside /home/discobot (e.g. /var/cache/apt), only chmod the leaf directory
		// to avoid changing permissions on system directories like /var.
		targetRoot := "/home/discobot"
		if !strings.HasPrefix(cachePath, "/home/discobot/") {
			targetRoot = filepath.Dir(cachePath)
		}
		chmodPathToRoot(cachePath, targetRoot, 0777)

		// Bind mount the cache directory
		if err := syscall.Mount(source, cachePath, "none", syscall.MS_BIND, ""); err != nil {
			fmt.Printf("discobot-agent: warning: failed to bind mount %s to %s: %v\n", source, cachePath, err)
			continue
		}

		mounted++
	}

	if mounted > 0 {
		fmt.Printf("discobot-agent: mounted %d cache directories\n", mounted)
	}

	return nil
}

// chmodPathToRoot sets permissions on path and all parent directories up to (but not including) root.
// This ensures all intermediate directories created by MkdirAll have the correct permissions.
func chmodPathToRoot(path, root string, mode os.FileMode) {
	// Clean paths to normalize them
	path = filepath.Clean(path)
	root = filepath.Clean(root)

	// Walk up the directory tree from path to root
	current := path
	for current != root && current != "/" && current != "." {
		if err := os.Chmod(current, mode); err != nil {
			// Don't log every error as it's noisy; the leaf chmod failure is logged elsewhere
			break
		}
		current = filepath.Dir(current)
	}
}
