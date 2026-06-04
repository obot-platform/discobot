//go:build windows

package main

import (
	"context"
	"log"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/hcs"
	"github.com/obot-platform/discobot/server/internal/sandbox/vm"
	"github.com/obot-platform/discobot/server/internal/sandbox/wsl"
	"github.com/obot-platform/discobot/server/internal/startup"
)

func registerPrimarySandboxProvider(
	cfg *config.Config,
	sandboxProviderManager *sandbox.ProviderManager,
	sessionProjectResolver func(context.Context, string) (string, error),
	providerResourceResolver vm.ProviderResourceResolver,
	systemManager *startup.SystemManager,
) {
	registerHCSSandboxProvider(cfg, sandboxProviderManager, sessionProjectResolver, providerResourceResolver, systemManager)
	registerWSLSandboxProvider(cfg, sandboxProviderManager, sessionProjectResolver, providerResourceResolver, systemManager)
}

func registerHCSSandboxProvider(
	cfg *config.Config,
	sandboxProviderManager *sandbox.ProviderManager,
	sessionProjectResolver func(context.Context, string) (string, error),
	providerResourceResolver vm.ProviderResourceResolver,
	systemManager *startup.SystemManager,
) {
	if cfg.HCSRootDiskPath == "" {
		log.Printf("HCS sandbox provider not configured: HCS_ROOT_DISK_PATH is empty")
		return
	}

	hcsCfg := &vm.Config{
		DataDir:       cfg.HCSDataDir,
		ConsoleLogDir: cfg.HCSConsoleLogDir,
		KernelPath:    cfg.HCSKernelPath,
		InitrdPath:    cfg.HCSInitrdPath,
		BaseDiskPath:  cfg.HCSRootDiskPath,
		LauncherPath:  cfg.HCSLauncherPath,
		HomeDir:       cfg.HCSHomeDir,
		CPUCount:      cfg.HCSCPUCount,
		MemoryMB:      cfg.HCSMemoryMB,
		DataDiskGB:    cfg.HCSDataDiskGB,
		IdleTimeout:   cfg.HCSIdleTimeout.String(),
	}

	hcsProvider, err := hcs.NewProvider(configForSandboxProvider(cfg, "hcs"), hcsCfg, sessionProjectResolver, providerResourceResolver, systemManager)
	if err != nil {
		log.Printf("Warning: Failed to initialize HCS sandbox provider: %v", err)
		return
	}

	sandboxProviderManager.RegisterProvider("hcs", hcsProvider)
	log.Printf("HCS sandbox provider initialized (state: %s)", hcsProvider.Status().State)
}

func registerWSLSandboxProvider(
	cfg *config.Config,
	sandboxProviderManager *sandbox.ProviderManager,
	sessionProjectResolver func(context.Context, string) (string, error),
	providerResourceResolver vm.ProviderResourceResolver,
	systemManager *startup.SystemManager,
) {
	if cfg.WSLRootfsPath != "" {
		log.Printf("WSL runtime rootfs source: local archive %s", cfg.WSLRootfsPath)
	} else {
		log.Printf("WSL runtime rootfs source: image %s", cfg.WSLImageRef)
	}

	wslProvider, err := wsl.NewProvider(configForSandboxProvider(cfg, "wsl"), sessionProjectResolver, providerResourceResolver, systemManager)
	if err != nil {
		log.Printf("Warning: Failed to initialize WSL sandbox provider: %v", err)
		return
	}

	sandboxProviderManager.RegisterProvider("wsl", wslProvider)
	log.Printf("WSL sandbox provider initialized (state: %s)", wslProvider.Status().State)
}
