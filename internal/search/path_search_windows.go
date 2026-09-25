//go:build windows

package search

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	userEnvironmentPathKey   = `Environment`
	systemEnvironmentPathKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
)

// systemAndUserPathDirectories 同时读取进程 PATH、用户 PATH 和系统 PATH。
// 进程 PATH 通常已经是二者合并后的值，但直接读取注册表可以覆盖尚未重启
// Explorer/终端的环境变量改动，也能明确补齐两类目录。
func systemAndUserPathDirectories() []string {
	pathValues := []string{os.Getenv("PATH")}
	pathValues = append(pathValues, registryPath(registry.CURRENT_USER, userEnvironmentPathKey))
	pathValues = append(pathValues, registryPath(registry.LOCAL_MACHINE, systemEnvironmentPathKey))

	directories := make([]string, 0)
	for _, value := range pathValues {
		for _, dir := range strings.Split(value, ";") {
			directories = append(directories, expandWindowsEnvironmentVariables(dir))
		}
	}
	return uniqueDirectories(directories)
}

func registryPath(root registry.Key, path string) string {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Path")
	if err != nil {
		return ""
	}
	return value
}

func expandWindowsEnvironmentVariables(value string) string {
	for start := strings.Index(value, "%"); start >= 0; {
		endOffset := strings.Index(value[start+1:], "%")
		if endOffset < 0 {
			break
		}
		end := start + 1 + endOffset
		name := value[start+1 : end]
		if replacement := os.Getenv(name); replacement != "" {
			value = value[:start] + replacement + value[end+1:]
			start = strings.Index(value, "%")
			continue
		}
		start = strings.Index(value[end+1:], "%")
		if start >= 0 {
			start += end + 1
		}
	}
	return value
}
