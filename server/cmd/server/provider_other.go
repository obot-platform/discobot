//go:build !darwin

package main

import (
	"context"
	"log"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/docker"
	"github.com/obot-platform/discobot/server/internal/startup"
)

func registerPrimarySandboxProvider(
	cfg *config.Config,
	sandboxManager *sandbox.Manager,
	sessionProjectResolver func(context.Context, string) (string, error),
	systemManager *startup.SystemManager,
) {
	dockerProvider, err := docker.NewProvider(cfg, sessionProjectResolver, docker.WithSystemManager(systemManager))
	if err != nil {
		log.Printf("Warning: Failed to initialize Docker sandbox provider: %v", err)
		return
	}

	sandboxManager.RegisterProvider("docker", dockerProvider)
	log.Printf("Docker sandbox provider initialized (image: %s)", cfg.SandboxImage)
}
