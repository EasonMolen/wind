//go:build windows

package everythingruntime

import (
	"context"
	"errors"
	"testing"
)

func TestStartReportsMissingPortableRuntime(t *testing.T) {
	manager := New(t.TempDir() + `\Everything.exe`)
	err := manager.Start(context.Background())
	if !errors.Is(err, ErrRuntimeNotFound) {
		t.Fatalf("Start error = %v, want ErrRuntimeNotFound", err)
	}
}
