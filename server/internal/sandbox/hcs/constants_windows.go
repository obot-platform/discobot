//go:build windows

package hcs

import "github.com/obot-platform/discobot/server/internal/sandbox/vm"

const (
	dockerSockPort    = 2375
	defaultDataDiskGB = 100
)

func mergeResources(defaults vm.ProviderResourceConfig, resolved vm.ProviderResourceConfig) vm.ProviderResourceConfig {
	if resolved.CPUCount > 0 {
		defaults.CPUCount = resolved.CPUCount
	}
	if resolved.MemoryMB > 0 {
		defaults.MemoryMB = resolved.MemoryMB
	}
	if resolved.DataDiskGB > 0 {
		defaults.DataDiskGB = resolved.DataDiskGB
	}
	return defaults
}
