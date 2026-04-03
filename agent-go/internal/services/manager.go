package services

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// subscriber receives live output events from a managed service.
type subscriber struct {
	ch chan OutputEvent
}

// managedService tracks a running service process and its subscribers.
type managedService struct {
	mu          sync.Mutex
	service     ServiceInfo
	process     *os.Process
	closeCh     chan struct{} // closed when process exits
	subscribers []*subscriber
}

// broadcast sends an event to all subscribers.
func (m *managedService) broadcast(event OutputEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscribers {
		select {
		case sub.ch <- event:
		default:
			// drop if subscriber is slow
		}
	}
}

// addSubscriber registers a new subscriber and returns its channel and unsubscribe func.
func (m *managedService) addSubscriber() (ch <-chan OutputEvent, unsubscribe func()) {
	sub := &subscriber{ch: make(chan OutputEvent, 256)}
	m.mu.Lock()
	m.subscribers = append(m.subscribers, sub)
	m.mu.Unlock()

	return sub.ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for i, s := range m.subscribers {
			if s == sub {
				m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
				break
			}
		}
	}
}

// Manager handles service discovery, process lifecycle, and output streaming.
type Manager struct {
	mu       sync.RWMutex
	services map[string]*managedService // keyed by service ID
}

// NewManager creates a new service Manager.
func NewManager() *Manager {
	return &Manager{
		services: make(map[string]*managedService),
	}
}

// desktopService is the built-in VNC desktop service.
var desktopService = ServiceInfo{
	ID:      "discobot-desktop",
	Name:    "Desktop",
	HTTP:    6080,
	Path:    "",
	Status:  "running",
	Passive: true,
}

// isDesktopAvailable checks if x11vnc is installed and executable.
func isDesktopAvailable() bool {
	info, err := os.Stat("/usr/bin/x11vnc")
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}

// GetServices returns all discovered services merged with running state.
func (mgr *Manager) GetServices(workspaceRoot string) ([]ServiceInfo, error) {
	servicesDir := filepath.Join(workspaceRoot, ServicesDir)
	discovered, err := DiscoverServices(servicesDir)
	if err != nil {
		return nil, err
	}

	var result []ServiceInfo

	// Add desktop service if available
	if isDesktopAvailable() {
		result = append(result, desktopService)
	}

	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	for _, svc := range discovered {
		if managed, ok := mgr.services[svc.ID]; ok {
			managed.mu.Lock()
			result = append(result, managed.service)
			managed.mu.Unlock()
		} else {
			result = append(result, svc)
		}
	}

	return result, nil
}

// GetService returns a single service by ID.
func (mgr *Manager) GetService(workspaceRoot, serviceID string) (*ServiceInfo, error) {
	if serviceID == "discobot-desktop" {
		if isDesktopAvailable() {
			svc := desktopService
			return &svc, nil
		}
		return nil, nil
	}

	// Check running services first
	mgr.mu.RLock()
	if managed, ok := mgr.services[serviceID]; ok {
		managed.mu.Lock()
		svc := managed.service
		managed.mu.Unlock()
		mgr.mu.RUnlock()
		return &svc, nil
	}
	mgr.mu.RUnlock()

	// Fall back to discovery
	services, err := mgr.GetServices(workspaceRoot)
	if err != nil {
		return nil, err
	}
	for i := range services {
		if services[i].ID == serviceID {
			return &services[i], nil
		}
	}
	return nil, nil
}

// StartService starts a service by ID. Returns the service info or an error code.
func (mgr *Manager) StartService(workspaceRoot, serviceID string) (*ServiceInfo, string, error) {
	// Check if already running
	mgr.mu.RLock()
	if managed, ok := mgr.services[serviceID]; ok {
		managed.mu.Lock()
		status := managed.service.Status
		pid := managed.service.PID
		managed.mu.Unlock()
		mgr.mu.RUnlock()

		if status == "running" || status == "starting" {
			return nil, "service_already_running", fmt.Errorf("service %s already running (pid %d)", serviceID, pid)
		}
	} else {
		mgr.mu.RUnlock()
	}

	// Discover service
	servicesDir := filepath.Join(workspaceRoot, ServicesDir)
	discovered, err := DiscoverServices(servicesDir)
	if err != nil {
		return nil, "", err
	}

	var svcTemplate *ServiceInfo
	for i := range discovered {
		if discovered[i].ID == serviceID {
			svcTemplate = &discovered[i]
			break
		}
	}
	if svcTemplate == nil {
		return nil, "service_not_found", fmt.Errorf("service %s not found", serviceID)
	}

	// Clear previous output
	clearOutput(serviceID)

	// Spawn the service
	mgr.spawnService(workspaceRoot, *svcTemplate)

	svc := ServiceInfo{
		ID:     serviceID,
		Status: "starting",
	}
	return &svc, "", nil
}

// StopService stops a running service.
func (mgr *Manager) StopService(serviceID string) (string, error) {
	mgr.mu.RLock()
	managed, ok := mgr.services[serviceID]
	mgr.mu.RUnlock()

	if !ok {
		return "service_not_found", fmt.Errorf("service %s not found", serviceID)
	}

	managed.mu.Lock()
	status := managed.service.Status
	managed.mu.Unlock()

	if status != "running" && status != "starting" {
		return "service_not_running", fmt.Errorf("service %s is not running", serviceID)
	}

	managed.mu.Lock()
	managed.service.Status = "stopping"
	proc := managed.process
	managed.mu.Unlock()

	if proc != nil {
		// Kill process group
		_ = killProcessGroup(proc.Pid, syscall.SIGTERM)

		// SIGKILL after 5 seconds if still running
		go func() {
			time.Sleep(5 * time.Second)
			managed.mu.Lock()
			s := managed.service.Status
			managed.mu.Unlock()
			if s == "stopping" {
				_ = killProcessGroup(proc.Pid, syscall.SIGKILL)
			}
		}()
	}

	return "", nil
}

// Subscribe returns a channel of live output events for a service and an unsubscribe func.
// Returns nil channels if the service is not managed (not running).
func (mgr *Manager) Subscribe(serviceID string) (<-chan OutputEvent, func(), <-chan struct{}) {
	mgr.mu.RLock()
	managed, ok := mgr.services[serviceID]
	mgr.mu.RUnlock()

	if !ok {
		return nil, func() {}, nil
	}

	ch, unsub := managed.addSubscriber()
	return ch, unsub, managed.closeCh
}

// GetServiceOutput returns stored output events from the JSONL file.
func (mgr *Manager) GetServiceOutput(serviceID string) []OutputEvent {
	return readEvents(serviceID)
}

// IsManaged returns whether a service is currently in the managed services map.
func (mgr *Manager) IsManaged(serviceID string) bool {
	mgr.mu.RLock()
	_, ok := mgr.services[serviceID]
	mgr.mu.RUnlock()
	return ok
}

// spawnService starts a service process and registers it in the manager.
func (mgr *Manager) spawnService(workspaceRoot string, svcTemplate ServiceInfo) {
	svc := svcTemplate
	svc.Status = "starting"
	svc.StartedAt = time.Now().UTC().Format(time.RFC3339)

	command, args := buildServiceCommand(svcTemplate.Path)
	cmd := exec.Command(command, args...)
	cmd.Dir = workspaceRoot
	cmd.Env = os.Environ()
	setSysProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("services: failed to create stdout pipe for %s: %v", svc.ID, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("services: failed to create stderr pipe for %s: %v", svc.ID, err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("services: failed to start %s: %v", svc.ID, err)
		appendEvent(svc.ID, newErrorEvent(err.Error()))
		return
	}

	svc.PID = cmd.Process.Pid
	svc.Status = "running"

	managed := &managedService{
		service: svc,
		process: cmd.Process,
		closeCh: make(chan struct{}),
	}

	mgr.mu.Lock()
	mgr.services[svc.ID] = managed
	mgr.mu.Unlock()

	emitEvent := func(event OutputEvent) {
		appendEvent(svc.ID, event)
		managed.broadcast(event)
	}

	// Read stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			emitEvent(newStdoutEvent(scanner.Text()))
		}
	}()

	// Read stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			emitEvent(newStderrEvent(scanner.Text()))
		}
	}()

	// Wait for process exit
	go func() {
		err := cmd.Wait()

		// Determine the exit event outside the lock to avoid deadlock:
		// emitEvent → broadcast also acquires managed.mu.
		var event OutputEvent
		managed.mu.Lock()
		managed.service.Status = "stopped"
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				managed.service.ExitCode = &code
				event = newExitEvent(&code)
			} else {
				event = newErrorEvent(err.Error())
			}
		} else {
			code := 0
			managed.service.ExitCode = &code
			event = newExitEvent(&code)
		}
		managed.mu.Unlock()

		emitEvent(event)

		close(managed.closeCh)

		// Grace period: remove from map after 30 seconds
		time.AfterFunc(30*time.Second, func() {
			mgr.mu.Lock()
			if current, ok := mgr.services[svc.ID]; ok && current == managed {
				delete(mgr.services, svc.ID)
			}
			mgr.mu.Unlock()
		})
	}()
}

func buildServiceCommand(path string) (string, []string) {
	if runtime.GOOS != "windows" {
		return path, nil
	}

	interpreter, args := parseServiceShebang(path)
	switch interpreter {
	case "bash", "sh", "zsh":
		return "bash", append(args, path)
	case "":
		return path, nil
	default:
		return interpreter, append(args, path)
	}
}

func parseServiceShebang(path string) (string, []string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}

	line, _, _ := strings.Cut(string(content), "\n")
	if !strings.HasPrefix(line, "#!") {
		return "", nil
	}

	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
	if len(fields) == 0 {
		return "", nil
	}

	interpreter := filepath.Base(fields[0])
	args := fields[1:]
	if interpreter == "env" {
		for len(args) > 0 && args[0] == "-S" {
			args = args[1:]
		}
		if len(args) == 0 {
			return "", nil
		}
		interpreter = filepath.Base(args[0])
		args = args[1:]
	}

	return strings.ToLower(interpreter), args
}
