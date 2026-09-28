//go:build windows

package search

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const appPathsRegistryKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`

// searchInstalledApplicationEntries discovers launchers that Everything often
// cannot expose: App Paths registrations (including Office) and Start Menu
// shortcuts (including packaged Microsoft Store applications).
func searchInstalledApplicationEntries(keyword string) []ResultSearch {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}

	results := make([]ResultSearch, 0)
	results = append(results, applicationEntriesFromAppPaths(keyword)...)
	results = append(results, applicationEntriesFromStartMenu(keyword)...)
	results = append(results, applicationEntriesFromStartApps(keyword)...)
	return uniqueApplicationEntries(results)
}

func applicationEntriesFromAppPaths(keyword string) []ResultSearch {
	type appPathsRoot struct {
		root  registry.Key
		flags uint32
	}

	roots := []appPathsRoot{
		{root: registry.CURRENT_USER, flags: registry.QUERY_VALUE | registry.ENUMERATE_SUB_KEYS},
		{root: registry.LOCAL_MACHINE, flags: registry.QUERY_VALUE | registry.ENUMERATE_SUB_KEYS},
		// Some 32-bit applications, including older Office installations, only
		// register beneath the 32-bit registry view on 64-bit Windows.
		{root: registry.LOCAL_MACHINE, flags: registry.QUERY_VALUE | registry.ENUMERATE_SUB_KEYS | registry.WOW64_32KEY},
	}

	results := make([]ResultSearch, 0)
	for _, source := range roots {
		key, err := registry.OpenKey(source.root, appPathsRegistryKey, source.flags)
		if err != nil {
			continue
		}

		names, err := key.ReadSubKeyNames(-1)
		if err == nil {
			for _, name := range names {
				if !matchesInstalledApplicationName(name, keyword) {
					continue
				}
				entryKey, err := registry.OpenKey(key, name, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				value, _, err := entryKey.GetStringValue("")
				entryKey.Close()
				if err != nil {
					continue
				}
				if result, ok := applicationEntryFromExecutablePath(value); ok {
					results = append(results, result)
				}
			}
		}
		key.Close()
	}
	return results
}

func applicationEntryFromExecutablePath(value string) (ResultSearch, bool) {
	path := executablePathFromCommand(value)
	if path == "" {
		return ResultSearch{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ResultSearch{}, false
	}
	return ResultSearch{
		FullPath:           path,
		FileName:           filepath.Base(path),
		Path:               filepath.Dir(path),
		Size:               info.Size(),
		ModifiedTime:       info.ModTime(),
		IsApplicationEntry: true,
	}, true
}

func executablePathFromCommand(value string) string {
	value = strings.TrimSpace(expandWindowsEnvironmentVariables(value))
	if value == "" {
		return ""
	}
	if value[0] == '"' {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return value[1 : end+1]
		}
		return ""
	}

	lower := strings.ToLower(value)
	if end := strings.Index(lower, ".exe"); end >= 0 {
		return strings.TrimSpace(value[:end+len(".exe")])
	}
	return value
}

func applicationEntriesFromStartMenu(keyword string) []ResultSearch {
	results := make([]ResultSearch, 0)
	for _, root := range startMenuDirectories() {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry == nil || entry.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".lnk") || !matchesInstalledApplicationName(entry.Name(), keyword) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			results = append(results, ResultSearch{
				FullPath:           path,
				FileName:           entry.Name(),
				Path:               filepath.Dir(path),
				Size:               info.Size(),
				ModifiedTime:       info.ModTime(),
				IsApplicationEntry: true,
			})
			return nil
		})
	}
	return results
}

type startApp struct {
	Name  string
	AppID string
}

var cachedStartApps struct {
	sync.Once
	entries []startApp
}

// applicationEntriesFromStartApps covers packaged apps whose executable lives
// in the protected WindowsApps directory. Get-StartApps exposes their AppUser
// Model IDs, which Explorer can launch through shell:AppsFolder.
func applicationEntriesFromStartApps(keyword string) []ResultSearch {
	cachedStartApps.Do(loadStartApps)

	results := make([]ResultSearch, 0)
	for _, app := range cachedStartApps.entries {
		if !matchesInstalledApplicationName(app.Name, keyword) {
			continue
		}
		if filepath.IsAbs(app.AppID) {
			if path, ok := executableStartAppPath(app.AppID); ok {
				if result, ok := applicationEntryFromExecutablePath(path); ok {
					results = append(results, result)
				}
			}
			continue
		}
		if app.AppID == "" {
			continue
		}
		results = append(results, ResultSearch{
			FullPath:           "shell:AppsFolder\\" + app.AppID,
			FileName:           app.Name,
			Path:               "Start Apps",
			IsApplicationEntry: true,
		})
	}
	return results
}

func loadStartApps() {
	output, err := startAppsCommand().Output()
	if err != nil || len(output) == 0 {
		return
	}

	var apps []startApp
	if err := json.Unmarshal(output, &apps); err == nil {
		cachedStartApps.entries = validStartApps(apps)
		return
	}

	var app startApp
	if err := json.Unmarshal(output, &app); err == nil {
		cachedStartApps.entries = validStartApps([]startApp{app})
	}
}

func startAppsCommand() *exec.Cmd {
	const script = "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); Get-StartApps | Select-Object Name,AppID | ConvertTo-Json -Compress"
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command
}

func validStartApps(apps []startApp) []startApp {
	result := make([]startApp, 0, len(apps))
	for _, app := range apps {
		app.Name = strings.TrimSpace(app.Name)
		app.AppID = strings.TrimSpace(app.AppID)
		if app.Name != "" && app.AppID != "" {
			result = append(result, app)
		}
	}
	return result
}

func executableStartAppPath(appID string) (string, bool) {
	appID = strings.TrimSpace(appID)
	if !filepath.IsAbs(appID) {
		return "", false
	}
	extension := strings.ToLower(filepath.Ext(appID))
	switch extension {
	case ".exe", ".com", ".bat", ".cmd":
		return appID, true
	default:
		return "", false
	}
}

func startMenuDirectories() []string {
	directories := []string{
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
	}
	return uniqueDirectories(directories)
}

func matchesInstalledApplicationName(name, keyword string) bool {
	baseName := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if baseName == "" || keyword == "" {
		return false
	}
	if strings.Contains(baseName, keyword) {
		return true
	}

	// POWERPNT.EXE is Microsoft's executable name for PowerPoint, so a normal
	// user query for "powerpoint" would otherwise miss the actual executable.
	aliases := map[string][]string{
		"powerpnt": {"powerpoint", "ppt"},
		"winword":  {"word"},
		"msaccess": {"access"},
	}
	for executableName, names := range aliases {
		if !strings.Contains(baseName, executableName) {
			continue
		}
		for _, alias := range names {
			if strings.Contains(alias, keyword) || strings.Contains(keyword, alias) {
				return true
			}
		}
	}
	return false
}

func uniqueApplicationEntries(entries []ResultSearch) []ResultSearch {
	seen := make(map[string]struct{}, len(entries))
	result := make([]ResultSearch, 0, len(entries))
	for _, entry := range entries {
		key := strings.ToLower(filepath.Clean(entry.FullPath))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entry)
	}
	return result
}
