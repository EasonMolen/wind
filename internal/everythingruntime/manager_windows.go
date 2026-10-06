//go:build windows

// Package everythingruntime starts an installed Everything instance when
// available, then falls back to the portable copy shipped beside NewWind. It
// deliberately does not install a Windows service or make registry changes.
package everythingruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

var ErrRuntimeNotFound = errors.New("portable Everything runtime was not found")

type Manager struct {
	executablePath string
	mu             sync.Mutex
	started        bool
}

func New(executablePath string) *Manager {
	return &Manager{executablePath: executablePath}
}

// DefaultExecutablePath returns the runtime location in a packaged release.
func DefaultExecutablePath() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(executable), "runtime", "everything.exe")
}

// Start launches Everything without showing its search window. An installed
// copy is preferred to the bundled portable copy. The context controls whether
// launch may begin; canceling it must not stop the background process.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	candidates := []string{installedExecutablePath(), m.executablePath}
	var startErrors []error
	seen := make(map[string]struct{}, len(candidates))
	for _, executable := range candidates {
		if executable == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(executable))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		info, err := os.Stat(executable)
		if err != nil || info.IsDir() {
			startErrors = append(startErrors, fmt.Errorf("Everything executable not found: %s", executable))
			continue
		}

		// This is a long-lived background process. CommandContext would kill it
		// as soon as the caller's short startup timeout is canceled.
		cmd := exec.Command(executable, "-startup", "-first-instance")
		cmd.Dir = filepath.Dir(executable)
		if err = cmd.Start(); err != nil {
			startErrors = append(startErrors, fmt.Errorf("start Everything from %s: %w", executable, err))
			continue
		}

		m.started = true
		go func() {
			_ = cmd.Wait()
			m.mu.Lock()
			m.started = false
			m.mu.Unlock()
		}()
		return nil
	}

	if len(startErrors) == 0 {
		return ErrRuntimeNotFound
	}
	return errors.Join(startErrors...)
}

func installedExecutablePath() string {
	const installRegistryPath = `SOFTWARE\voidtools\Everything`
	for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY, 0} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, installRegistryPath, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		location, _, err := key.GetStringValue("InstallLocation")
		key.Close()
		if err != nil || strings.TrimSpace(location) == "" {
			continue
		}

		executable := filepath.Join(location, "Everything.exe")
		info, err := os.Stat(executable)
		if err == nil && !info.IsDir() {
			return executable
		}
	}
	return ""
}
