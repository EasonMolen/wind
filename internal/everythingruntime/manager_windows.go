//go:build windows

// Package everythingruntime manages the portable Everything process shipped
// beside NewWind. It deliberately does not install a Windows service or make
// registry changes.
package everythingruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	return filepath.Join(filepath.Dir(executable), "runtime", "Everything.exe")
}

// Start launches Everything without showing its search window. -first-instance
// makes the command a no-op if an Everything instance is already available.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}
	if m.executablePath == "" {
		return ErrRuntimeNotFound
	}
	info, err := os.Stat(m.executablePath)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%w: %s", ErrRuntimeNotFound, m.executablePath)
	}

	cmd := exec.CommandContext(ctx, m.executablePath, "-startup", "-first-instance")
	cmd.Dir = filepath.Dir(m.executablePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start portable Everything: %w", err)
	}
	m.started = true
	return nil
}
