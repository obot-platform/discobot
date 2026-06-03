//go:build windows

package hcs

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/vm"
	"github.com/obot-platform/discobot/server/internal/sysinfo"
)

const gracefulLauncherStopTimeout = 15 * time.Second

type VMManager struct {
	config vm.Config

	providerResourceResolver vm.ProviderResourceResolver

	projectVMs  map[string]*projectVM
	projectVMMu sync.RWMutex

	ready     chan struct{}
	readyOnce sync.Once
	initErr   error

	stopOnce sync.Once
}

func NewProvider(cfg *config.Config, vmConfig *vm.Config, resolver vm.SessionProjectResolver, resourceResolver vm.ProviderResourceResolver, systemManager vm.SystemManager) (*vm.Provider, error) {
	vmManager, err := NewVMManager(*vmConfig, resourceResolver)
	if err != nil {
		return nil, fmt.Errorf("failed to create HCS VM manager: %w", err)
	}

	opts := []vm.Option{
		vm.WithPostVMSetup(vm.StartProxyContainer(cfg.SandboxImage)),
		vm.WithProviderResourceResolver(resourceResolver),
		vm.WithProviderName("hcs"),
	}
	if vmConfig.IdleTimeout != "" {
		idleTimeout, err := time.ParseDuration(vmConfig.IdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid idle timeout %q: %w", vmConfig.IdleTimeout, err)
		}
		if idleTimeout > 0 {
			opts = append(opts, vm.WithIdleTimeout(idleTimeout))
		}
	}

	return vm.NewProvider(cfg, vmManager, resolver, systemManager, opts...), nil
}

func NewVMManager(cfg vm.Config, resolver vm.ProviderResourceResolver) (*VMManager, error) {
	mgr := &VMManager{
		config:                   cfg,
		providerResourceResolver: resolver,
		projectVMs:               make(map[string]*projectVM),
		ready:                    make(chan struct{}),
	}
	if strings.TrimSpace(cfg.BaseDiskPath) == "" {
		mgr.initErr = fmt.Errorf("HCS root disk path is required")
	}
	mgr.closeReady()
	return mgr, nil
}

func (m *VMManager) Ready() <-chan struct{} { return m.ready }
func (m *VMManager) Err() error             { return m.initErr }

func (m *VMManager) closeReady() {
	m.readyOnce.Do(func() { close(m.ready) })
}

func (m *VMManager) Status() sandbox.ProviderStatus {
	status := sandbox.ProviderStatus{Available: true}
	select {
	case <-m.ready:
		if m.initErr != nil {
			status.Available = false
			status.State = "failed"
			status.Message = m.initErr.Error()
			return status
		}
		status.State = "ready"
		status.Details = StatusDetails{Config: &ProviderConfigInfo{
			LauncherPath: m.launcherPath(),
			KernelPath:   m.config.KernelPath,
			RootDiskPath: m.config.BaseDiskPath,
			DataDir:      m.config.DataDir,
			MemoryMB:     m.defaultMemoryMB(),
			CPUCount:     m.defaultCPUCount(),
			DataDiskGB:   m.defaultDataDiskGB(),
		}}
	default:
		status.State = "initializing"
		status.Message = "HCS VM manager initializing"
	}
	return status
}

func (m *VMManager) GetOrCreateVM(ctx context.Context, projectID string) (vm.ProjectVM, error) {
	m.projectVMMu.Lock()
	defer m.projectVMMu.Unlock()
	if pvm, ok := m.projectVMs[projectID]; ok {
		return pvm, nil
	}
	pvm, err := m.createProjectVM(ctx, projectID)
	if err != nil {
		return nil, err
	}
	m.projectVMs[projectID] = pvm
	return pvm, nil
}

func (m *VMManager) GetVM(projectID string) (vm.ProjectVM, bool) {
	m.projectVMMu.RLock()
	defer m.projectVMMu.RUnlock()
	pvm, ok := m.projectVMs[projectID]
	return pvm, ok
}

func (m *VMManager) ListProjectIDs() []string {
	m.projectVMMu.RLock()
	ids := make(map[string]struct{}, len(m.projectVMs))
	for id := range m.projectVMs {
		ids[id] = struct{}{}
	}
	dataDir := m.config.DataDir
	m.projectVMMu.RUnlock()

	if dataDir != "" {
		matches, err := filepath.Glob(filepath.Join(dataDir, "project-*-data.vhdx"))
		if err != nil {
			log.Printf("Warning: Failed to glob persisted HCS data disks in %s: %v", dataDir, err)
		} else {
			for _, match := range matches {
				projectID := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(match), "project-"), "-data.vhdx")
				if projectID != "" {
					ids[projectID] = struct{}{}
				}
			}
		}
	}

	projectIDs := make([]string, 0, len(ids))
	for id := range ids {
		projectIDs = append(projectIDs, id)
	}
	sort.Strings(projectIDs)
	return projectIDs
}

func (m *VMManager) RemoveVM(projectID string) error {
	m.projectVMMu.Lock()
	defer m.projectVMMu.Unlock()
	pvm, ok := m.projectVMs[projectID]
	if !ok {
		return nil
	}
	if err := pvm.Shutdown(); err != nil {
		return err
	}
	delete(m.projectVMs, projectID)
	return nil
}

func (m *VMManager) Shutdown() {
	m.stopOnce.Do(func() {
		m.projectVMMu.Lock()
		defer m.projectVMMu.Unlock()
		for projectID, pvm := range m.projectVMs {
			log.Printf("Shutting down HCS project VM: %s", projectID)
			if err := pvm.Shutdown(); err != nil {
				log.Printf("Error stopping HCS project VM %s: %v", projectID, err)
			}
		}
		m.projectVMs = make(map[string]*projectVM)
	})
}

func (m *VMManager) ProviderResources(ctx context.Context, projectID string) (vm.ProviderResourceConfig, error) {
	resources := vm.ProviderResourceConfig{CPUCount: m.defaultCPUCount(), MemoryMB: m.defaultMemoryMB(), DataDiskGB: m.defaultDataDiskGB()}
	if m.providerResourceResolver == nil {
		return resources, nil
	}
	resolved, err := m.providerResourceResolver(ctx, projectID)
	if err != nil {
		return vm.ProviderResourceConfig{}, err
	}
	return mergeResources(resources, resolved), nil
}

func (m *VMManager) ResizeDataDisk(_ context.Context, projectID string, sizeGB int) error {
	dataDiskPath := filepath.Join(m.config.DataDir, fmt.Sprintf("project-%s-data.vhdx", projectID))
	if _, err := os.Stat(dataDiskPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat data disk: %w", err)
	}
	return resizeVHDX(dataDiskPath, sizeGB)
}

func (m *VMManager) createProjectVM(ctx context.Context, projectID string) (*projectVM, error) {
	resources, err := m.ProviderResources(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve provider resources: %w", err)
	}
	if err := os.MkdirAll(m.config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create HCS data directory: %w", err)
	}
	if err := os.MkdirAll(m.config.ConsoleLogDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create HCS console log directory: %w", err)
	}

	dataDiskPath := filepath.Join(m.config.DataDir, fmt.Sprintf("project-%s-data.vhdx", projectID))
	if err := ensureVHDX(dataDiskPath, resources.DataDiskGB); err != nil {
		return nil, fmt.Errorf("failed to ensure HCS data disk: %w", err)
	}

	vmID, err := newGUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate HCS VM id: %w", err)
	}

	consoleLogPath := filepath.Join(m.config.ConsoleLogDir, fmt.Sprintf("project-%s", projectID), "console.log")
	if err := os.MkdirAll(filepath.Dir(consoleLogPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create console log directory: %w", err)
	}
	consoleLog, err := os.OpenFile(consoleLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create console log: %w", err)
	}

	proc, err := m.startLauncher(projectID, vmID, dataDiskPath, resources, consoleLog)
	if err != nil {
		consoleLog.Close()
		return nil, err
	}

	pvm := &projectVM{projectID: projectID, vmID: vmID, proc: proc, consoleLog: consoleLog}
	if err := waitForDocker(ctx, pvm, projectID); err != nil {
		_ = pvm.Shutdown()
		return nil, fmt.Errorf("docker daemon not ready: %w", err)
	}
	return pvm, nil
}

func (m *VMManager) startLauncher(projectID string, vmID windows.GUID, dataDiskPath string, resources vm.ProviderResourceConfig, consoleLog *os.File) (*launcherProcess, error) {
	args := []string{
		"--id", formatGUID(vmID),
		"--root", m.config.BaseDiskPath,
		"--data", dataDiskPath,
		"--network", "user-vsock",
		"--root-device", "/dev/sda",
		"--root-fstype", "squashfs",
		"--processors", strconv.Itoa(resources.CPUCount),
		"--memory-mb", strconv.Itoa(resources.MemoryMB),
		"--vsock-port", strconv.Itoa(dockerSockPort),
	}
	launcherPath := m.launcherPath()
	if filepath.IsAbs(launcherPath) {
		args = append(args, "--gvproxy", filepath.Join(filepath.Dir(launcherPath), "gvproxy.exe"))
	}
	if m.config.KernelPath != "" {
		args = append(args, "--kernel", m.config.KernelPath)
	}
	if m.config.InitrdPath != "" {
		args = append(args, "--initrd", m.config.InitrdPath)
	} else {
		args = append(args, "--no-initrd")
	}
	// Host sharing is opt-in. Do not mount the Windows user profile by default,
	// since that can expose SSH keys, cloud credentials, browser data, and
	// other project secrets to the guest.
	if m.config.HomeDir != "" {
		args = append(args, "--share", "home="+m.config.HomeDir, "--append-kernel-cmdline", "discobot.homedir=/mnt/home")
	}

	cmd := exec.Command(launcherPath, args...)
	if filepath.IsAbs(launcherPath) {
		cmd.Dir = filepath.Dir(launcherPath)
	}
	cmd.Stdout = consoleLog
	cmd.Stderr = consoleLog
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}

	proc, err := startLauncherProcess(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start HCS launcher for project %s: %w", projectID, err)
	}
	log.Printf("Started HCS launcher for project %s (pid %d, vm %s)", projectID, cmd.Process.Pid, formatGUID(vmID))
	return proc, nil
}

func (m *VMManager) launcherPath() string {
	if m.config.LauncherPath != "" {
		return m.config.LauncherPath
	}
	return "HcsLinuxVmLauncher.exe"
}

func (m *VMManager) defaultCPUCount() int {
	if m.config.CPUCount > 0 {
		return m.config.CPUCount
	}
	return runtime.NumCPU()
}

func (m *VMManager) defaultMemoryMB() int {
	if m.config.MemoryMB > 0 {
		return m.config.MemoryMB
	}
	totalMem := sysinfo.TotalMemoryBytes()
	oneGB := uint64(1024 * 1024 * 1024)
	return int(((totalMem / 2) / oneGB) * oneGB / (1024 * 1024))
}

func (m *VMManager) defaultDataDiskGB() int {
	if m.config.DataDiskGB > 0 {
		return m.config.DataDiskGB
	}
	return defaultDataDiskGB
}

func waitForDocker(ctx context.Context, pvm *projectVM, projectID string) error {
	deadline := time.Now().Add(90 * time.Second)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for Docker daemon")
			}
			conn, err := pvm.DockerDialer()(ctx, "", "")
			if err != nil {
				log.Printf("HCS VM %s: waiting for Docker (connect failed: %v)", projectID, err)
				continue
			}
			_ = conn.Close()
			return nil
		}
	}
}

func ensureVHDX(path string, sizeGB int) error {
	if _, err := os.Stat(path); err == nil {
		return resizeVHDX(path, sizeGB)
	} else if !os.IsNotExist(err) {
		return err
	}
	return runPowerShell("New-VHD", "-Path", path, "-SizeBytes", strconv.FormatInt(int64(sizeGB)*1024*1024*1024, 10), "-Dynamic")
}

func resizeVHDX(path string, sizeGB int) error {
	return runPowerShell("Resize-VHD", "-Path", path, "-SizeBytes", strconv.FormatInt(int64(sizeGB)*1024*1024*1024, 10))
}

func runPowerShell(args ...string) error {
	cmdArgs := append([]string{"-NoProfile", "-NonInteractive", "-Command"}, args...)
	cmd := exec.Command("powershell.exe", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func serviceGUID(port uint32) windows.GUID {
	return windows.GUID{Data1: port, Data2: 0xfacb, Data3: 0x11e6, Data4: [8]byte{0xbd, 0x58, 0x64, 0x00, 0x6a, 0x79, 0x86, 0xd3}}
}

func formatGUID(g windows.GUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x", g.Data1, g.Data2, g.Data3, g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3], g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

var (
	ole32            = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateGUID = ole32.NewProc("CoCreateGuid")
)

func newGUID() (windows.GUID, error) {
	var g windows.GUID
	r1, _, _ := procCoCreateGUID.Call(uintptr(unsafe.Pointer(&g)))
	if r1 != 0 {
		return windows.GUID{}, windows.Errno(r1)
	}
	return g, nil
}

func sizeOf[T any]() int32 { return int32(unsafe.Sizeof(*new(T))) }
