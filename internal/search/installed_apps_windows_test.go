//go:build windows

package search

import "testing"

func TestMatchesInstalledApplicationNameRecognizesOfficeExecutableAliases(t *testing.T) {
	tests := []struct {
		name    string
		keyword string
		want    bool
	}{
		{name: "POWERPNT.EXE", keyword: "powerpoint", want: true},
		{name: "WINWORD.EXE", keyword: "word", want: true},
		{name: "EXCEL.EXE", keyword: "excel", want: true},
		{name: "POWERPNT.EXE", keyword: "excel", want: false},
	}
	for _, test := range tests {
		if got := matchesInstalledApplicationName(test.name, test.keyword); got != test.want {
			t.Errorf("matchesInstalledApplicationName(%q, %q) = %v, want %v", test.name, test.keyword, got, test.want)
		}
	}
}

func TestExecutablePathFromCommand(t *testing.T) {
	got := executablePathFromCommand(`"C:\Program Files\Microsoft Office\root\Office16\WINWORD.EXE" /n`)
	want := `C:\Program Files\Microsoft Office\root\Office16\WINWORD.EXE`
	if got != want {
		t.Fatalf("executablePathFromCommand() = %q, want %q", got, want)
	}
}

func TestExecutableStartAppPath(t *testing.T) {
	path, ok := executableStartAppPath("C:\\Tools\\tool.exe")
	if !ok || path != "C:\\Tools\\tool.exe" {
		t.Fatalf("executableStartAppPath() = (%q, %v), want executable path", path, ok)
	}
	if _, ok := executableStartAppPath("Microsoft.WindowsStore_8wekyb3d8bbwe!App"); ok {
		t.Fatal("packaged AppUserModelID must be launched through AppsFolder")
	}
}

func TestStartAppsCommandHidesWindow(t *testing.T) {
	if command := startAppsCommand(); command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("Get-StartApps command must run without a visible console window")
	}
}
