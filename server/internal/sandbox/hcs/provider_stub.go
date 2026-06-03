//go:build !windows

package hcs

import (
	"fmt"
	"runtime"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox/vm"
)

// NewProvider returns an error on non-Windows platforms.
func NewProvider(_ *config.Config, _ *vm.Config, _ vm.SessionProjectResolver, _ vm.ProviderResourceResolver, _ vm.SystemManager) (*vm.Provider, error) {
	return nil, fmt.Errorf("hcs sandbox provider is only available on Windows, current platform: %s", runtime.GOOS)
}
