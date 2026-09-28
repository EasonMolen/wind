package search

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// searchPathEntries 补齐 Everything 未索引目录中的可执行命令。
// PATH 的解析只扫描每个目录的第一层：这是 Windows 命令查找的实际规则，
// 也避免把整个 SDK、驱动或工具链递归扫描进搜索结果。
func searchPathEntries(keyword string) []ResultSearch {
	return searchPathEntriesInDirectories(keyword, systemAndUserPathDirectories())
}

// searchSupplementalLaunchers adds launch targets that Everything frequently
// cannot expose. PATH can include system directories, while Store apps are
// commonly surfaced as Start Menu shortcuts instead of indexable executables.
func searchSupplementalLaunchers(keyword string) []ResultSearch {
	results := searchPathEntries(keyword)
	return append(results, searchInstalledApplicationEntries(keyword)...)
}

func searchPathEntriesInDirectories(keyword string, directories []string) []ResultSearch {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return nil
	}

	seen := make(map[string]struct{})
	results := make([]ResultSearch, 0)
	for _, dir := range directories {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // PATH 中失效的目录不应影响主搜索
		}
		for _, entry := range entries {
			if entry.IsDir() || !matchesPathEntry(entry.Name(), keyword) {
				continue
			}

			fullPath := filepath.Join(dir, entry.Name())
			key := strings.ToLower(filepath.Clean(fullPath))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			info, err := entry.Info()
			if err != nil {
				continue
			}
			results = append(results, ResultSearch{
				FullPath:     fullPath,
				FileName:     entry.Name(),
				Path:         dir,
				Size:         info.Size(),
				ModifiedTime: info.ModTime(),
				CreatedTime:  time.Time{},
				IsPathEntry:  true,
			})
		}
	}

	return results
}

func matchesPathEntry(name, keyword string) bool {
	lowerName := strings.ToLower(name)
	baseName := strings.TrimSuffix(lowerName, strings.ToLower(filepath.Ext(lowerName)))
	if !strings.Contains(baseName, keyword) && !strings.Contains(lowerName, keyword) {
		return false
	}

	extension := strings.ToLower(filepath.Ext(name))
	for _, executableExtension := range pathExecutableExtensions() {
		if extension == executableExtension {
			return true
		}
	}
	return false
}

func pathExecutableExtensions() []string {
	const defaultExtensions = ".com;.exe;.bat;.cmd;.vbs;.vbe;.js;.jse;.wsf;.wsh;.msc"
	value := os.Getenv("PATHEXT")
	if value == "" {
		value = defaultExtensions
	}

	extensions := make([]string, 0, 12)
	for _, extension := range strings.Split(value, ";") {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension != "" {
			extensions = append(extensions, extension)
		}
	}
	return extensions
}

func uniqueDirectories(directories []string) []string {
	seen := make(map[string]struct{}, len(directories))
	result := make([]string, 0, len(directories))
	for _, dir := range directories {
		dir = strings.TrimSpace(strings.Trim(dir, `"`))
		if dir == "" {
			continue
		}
		cleaned := filepath.Clean(dir)
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}
