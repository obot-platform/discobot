package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/adrg/xdg"

	"github.com/obot-platform/discobot/server/internal/startup"
	"github.com/obot-platform/discobot/server/internal/version"
)

// ServerConfigResponse contains public server configuration for the frontend
type ServerConfigResponse struct {
	SSHPort       int    `json:"ssh_port"`
	HTTPPort      int    `json:"http_port"`
	HTTPSPort     int    `json:"https_port,omitempty"`
	HTTPSTLSMode  string `json:"https_tls_mode,omitempty"`
	PublicBaseURL string `json:"public_base_url"`
}

// GetServerConfig returns public server configuration
func (h *Handler) GetServerConfig(w http.ResponseWriter, _ *http.Request) {
	h.JSON(w, http.StatusOK, ServerConfigResponse{
		SSHPort:       h.cfg.SSHPort,
		HTTPPort:      h.cfg.Port,
		HTTPSPort:     h.cfg.HTTPSPort,
		HTTPSTLSMode:  h.cfg.HTTPSTLSMode,
		PublicBaseURL: h.cfg.PublicBaseURL(),
	})
}

// GetSystemStatus checks system requirements and returns status (including startup tasks)
func (h *Handler) GetSystemStatus(w http.ResponseWriter, _ *http.Request) {
	// Use system manager to get complete system status
	if h.systemManager != nil {
		status := h.systemManager.GetSystemStatus()
		h.JSON(w, http.StatusOK, status)
		return
	}

	// Fallback if system manager is not available
	h.JSON(w, http.StatusOK, startup.SystemStatusResponse{
		OK:       true,
		Messages: []startup.StatusMessage{},
	})
}

// SupportInfoResponse contains diagnostic information for debugging and support
type SupportInfoResponse struct {
	Version    string                       `json:"version"`
	Runtime    RuntimeInfo                  `json:"runtime"`
	Config     ConfigInfo                   `json:"config"`
	ServerLog  string                       `json:"server_log"`
	LogPath    string                       `json:"log_path"`
	LogExists  bool                         `json:"log_exists"`
	SystemInfo startup.SystemStatusResponse `json:"system_info"`
}

// RuntimeInfo contains Go runtime information
type RuntimeInfo struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	GoVersion    string `json:"go_version"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
}

// ConfigInfo contains sanitized configuration information
type ConfigInfo struct {
	Port               int      `json:"port"`
	HTTPSPort          int      `json:"https_port,omitempty"`
	HTTPSTLSMode       string   `json:"https_tls_mode,omitempty"`
	DatabaseDriver     string   `json:"database_driver"`
	AuthEnabled        bool     `json:"auth_enabled"`
	WorkspaceDir       string   `json:"workspace_dir"`
	SandboxImage       string   `json:"sandbox_image"`
	DesktopMode        bool     `json:"desktop_mode"`
	DesktopRuntime     string   `json:"desktop_runtime,omitempty"`
	TauriMode          bool     `json:"tauri_mode"`
	SSHEnabled         bool     `json:"ssh_enabled"`
	SSHPort            int      `json:"ssh_port"`
	DispatcherEnabled  bool     `json:"dispatcher_enabled"`
	AvailableProviders []string `json:"available_providers"`
	VZ                 *VZInfo  `json:"vz,omitempty"`
}

// VZInfo contains VZ-specific configuration and disk usage information
type VZInfo struct {
	ImageRef     string             `json:"image_ref"`
	DataDir      string             `json:"data_dir"`
	CPUCount     int                `json:"cpu_count"`
	MemoryMB     int                `json:"memory_mb"`
	DataDiskGB   int                `json:"data_disk_gb"`
	DiskUsage    *DiskUsageInfo     `json:"disk_usage,omitempty"`
	DataDisks    []DataDiskFileInfo `json:"data_disks,omitempty"`
	KernelPath   string             `json:"kernel_path,omitempty"`
	InitrdPath   string             `json:"initrd_path,omitempty"`
	BaseDiskPath string             `json:"base_disk_path,omitempty"`
}

// DiskUsageInfo contains filesystem usage statistics
type DiskUsageInfo struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// DataDiskFileInfo contains size information for a sparse data disk file
type DataDiskFileInfo struct {
	Path          string `json:"path"`
	ApparentBytes uint64 `json:"apparent_bytes"` // Logical file size
	ActualBytes   uint64 `json:"actual_bytes"`   // Actual disk usage (sparse-aware)
}

// GetSupportInfo returns comprehensive diagnostic information for debugging
func (h *Handler) GetSupportInfo(w http.ResponseWriter, _ *http.Request) {
	// Get runtime info
	runtimeInfo := RuntimeInfo{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
	}

	// Get sanitized config info
	var availableProviders []string
	if h.sandboxManager != nil {
		availableProviders = h.sandboxManager.ListProviders()
	}

	configInfo := ConfigInfo{
		Port:               h.cfg.Port,
		HTTPSPort:          h.cfg.HTTPSPort,
		HTTPSTLSMode:       h.cfg.HTTPSTLSMode,
		DatabaseDriver:     h.cfg.DatabaseDriver,
		AuthEnabled:        h.cfg.AuthEnabled,
		WorkspaceDir:       h.cfg.WorkspaceDir,
		SandboxImage:       h.cfg.SandboxImage,
		DesktopMode:        h.cfg.DesktopMode,
		DesktopRuntime:     h.cfg.DesktopRuntime,
		TauriMode:          h.cfg.DesktopRuntime == "tauri",
		SSHEnabled:         h.cfg.SSHEnabled,
		SSHPort:            h.cfg.SSHPort,
		DispatcherEnabled:  h.cfg.DispatcherEnabled,
		AvailableProviders: availableProviders,
	}

	// Add VZ info if on macOS
	if runtime.GOOS == "darwin" {
		vzInfo := &VZInfo{
			ImageRef:     h.cfg.VZImageRef,
			DataDir:      h.cfg.VZDataDir,
			CPUCount:     h.cfg.VZCPUCount,
			MemoryMB:     h.cfg.VZMemoryMB,
			DataDiskGB:   h.cfg.VZDataDiskGB,
			KernelPath:   h.cfg.VZKernelPath,
			InitrdPath:   h.cfg.VZInitrdPath,
			BaseDiskPath: h.cfg.VZBaseDiskPath,
		}

		// Get disk usage for VZ data directory
		if diskUsage := getDiskUsage(h.cfg.VZDataDir); diskUsage != nil {
			vzInfo.DiskUsage = diskUsage
		}

		// Scan for data disk files
		vzInfo.DataDisks = getDataDiskFiles(h.cfg.VZDataDir)

		configInfo.VZ = vzInfo
	}

	// Read server log file (Tauri sidecar log)
	logPath := filepath.Join(xdg.StateHome, "discobot", "logs", "server.log")
	logContent := ""
	logExists := false

	if logData, err := os.ReadFile(logPath); err == nil {
		logContent = string(logData)
		logExists = true
	}

	// Get system status from system manager
	var systemStatus startup.SystemStatusResponse
	if h.systemManager != nil {
		systemStatus = h.systemManager.GetSystemStatus()
	}

	response := SupportInfoResponse{
		Version:    version.Get(),
		Runtime:    runtimeInfo,
		Config:     configInfo,
		ServerLog:  logContent,
		LogPath:    logPath,
		LogExists:  logExists,
		SystemInfo: systemStatus,
	}

	h.JSON(w, http.StatusOK, response)
}

// getDiskUsage returns filesystem usage statistics for a given path
// Platform-specific implementations in status_unix.go and status_windows.go

// getDataDiskFiles scans for project data disk images and returns their size info.
// Platform-specific implementations in status_unix.go and status_windows.go
