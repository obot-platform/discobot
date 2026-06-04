package hcs

// StatusDetails contains HCS-specific status details returned in ProviderStatus.Details.
type StatusDetails struct {
	Config *ProviderConfigInfo `json:"config,omitempty"`
}

// ProviderConfigInfo contains HCS provider configuration information.
type ProviderConfigInfo struct {
	LauncherPath string `json:"launcher_path,omitempty"`
	KernelPath   string `json:"kernel_path,omitempty"`
	RootDiskPath string `json:"root_disk_path,omitempty"`
	DataDir      string `json:"data_dir,omitempty"`
	MemoryMB     int    `json:"memory_mb"`
	CPUCount     int    `json:"cpu_count"`
	DataDiskGB   int    `json:"data_disk_gb"`
}
