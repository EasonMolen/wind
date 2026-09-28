package search

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilterKeepsApplicationsAndRemovesSystemNoise(t *testing.T) {
	engine := NewFilterEngine(20)
	results := []ResultSearch{
		{FullPath: `C:\Windows\System32\qq.exe`, FileName: "qq.exe", Path: `C:\Windows\System32`},
		{FullPath: `C:\Program Files\Tencent\QQ\QQ.exe`, FileName: "QQ.exe", Path: `C:\Program Files\Tencent\QQ`},
		{FullPath: `C:\Programs File\MyApp\myapp.exe`, FileName: "myapp.exe", Path: `C:\Programs File\MyApp`},
		{FullPath: `C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`, FileName: "nvidia-smi.exe", Path: `C:\Program Files\NVIDIA Corporation\NVSMI`},
		{FullPath: `D:\Projects\demo\.git\config`, FileName: "config", Path: `D:\Projects\demo\.git`},
	}

	got := engine.Filter(results, "app")
	gotPaths := make(map[string]bool, len(got))
	for _, result := range got {
		gotPaths[result.FullPath] = true
	}

	if !gotPaths[`C:\Programs File\MyApp\myapp.exe`] {
		t.Fatalf("custom Program Files-style directory was filtered out: %#v", got)
	}
	if gotPaths[`C:\Windows\System32\qq.exe`] || gotPaths[`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`] || gotPaths[`D:\Projects\demo\.git\config`] {
		t.Fatalf("system, driver, or metadata paths were retained: %#v", got)
	}
}

func TestPathSearchReturnsOnlyMatchingLaunchers(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tool.exe", "tool.cmd", "note.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := searchPathEntriesInDirectories("tool", []string{dir, dir})
	if len(got) != 2 {
		t.Fatalf("got %d PATH results, want 2: %#v", len(got), got)
	}
	for _, result := range got {
		if !result.IsPathEntry {
			t.Fatalf("PATH result was not marked: %#v", result)
		}
	}
}

func TestFilterKeepsPathEntriesInSystemDirectories(t *testing.T) {
	engine := NewFilterEngine(20)
	results := []ResultSearch{{
		FullPath:    `C:\Windows\System32\cmd.exe`,
		FileName:    "cmd.exe",
		Path:        `C:\Windows\System32`,
		IsPathEntry: true,
	}}

	got := engine.Filter(results, "cmd")
	if len(got) != 1 || got[0].FullPath != results[0].FullPath {
		t.Fatalf("PATH entry in a system directory was filtered out: %#v", got)
	}
}
