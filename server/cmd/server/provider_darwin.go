//go:build darwin

package main

import (
	"context"
	"log"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/vm"
	"github.com/obot-platform/discobot/server/internal/sandbox/vz"
	"github.com/obot-platform/discobot/server/internal/startup"
)

func registerPrimarySandboxProvider(
	cfg *config.Config,
	sandboxManager *sandbox.Manager,
	sessionProjectResolver func(context.Context, string) (string, error),
	systemManager *startup.SystemManager,
) {
	vzCfg := &vm.Config{
		DataDir:       cfg.VZDataDir,
		ConsoleLogDir: cfg.VZConsoleLogDir,
		KernelPath:    cfg.VZKernelPath,
		InitrdPath:    cfg.VZInitrdPath,
		BaseDiskPath:  cfg.VZBaseDiskPath,
		ImageRef:      cfg.VZImageRef,
		HomeDir:       cfg.VZHomeDir,
		CPUCount:      cfg.VZCPUCount,
		MemoryMB:      cfg.VZMemoryMB,
		DataDiskGB:    cfg.VZDataDiskGB,
	}

	vmProvider, err := vz.NewProvider(cfg, vzCfg, sessionProjectResolver, systemManager)
	if err != nil {
		log.Printf("Warning: Failed to initialize VZ sandbox provider: %v", err)
		return
	}

	sandboxManager.RegisterProvider("vz", vmProvider)
	if vmProvider.IsReady() {
		log.Printf("VZ sandbox provider initialized and ready")
		return
	}

	log.Printf("VZ sandbox provider registered (images downloading in background)")
}
