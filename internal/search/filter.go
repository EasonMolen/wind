package search

import (
	"os"
	"sort"
	"strings"
	"time"
)

// 预定义系统路径（统一小写）。
//
// 注意：这里故意不排除 Program Files、Program Files (x86) 和 AppData。
// 它们既会产生大量噪声，也是 QQ、微信、用户自定义安装目录等应用的常见位置。
// 噪声资源在 calcula teScore 中降权，而不是在这里一刀切地丢弃。
var defaultSystemPatterns = []string{
	`c:\windows\`,
	`c:\programdata\`,
	`c:\$recycle.bin\`,
	`c:\system volume information\`,
	`c:\recovery\`,
	`c:\$winreagent\`,
}

// 这些是路径中的完整目录段，不会误伤名字里恰好包含这些字母的文件。
var defaultJunkPathSegments = []string{
	`\temp\`,
	`\cache\`,
	`\node_modules\`,
	`\.git\`,
}

// 驱动与硬件组件的安装目录通常没有直接启动价值；保留普通的软件安装目录。
var defaultDriverPathSegments = []string{
	`\nvidia corporation\`,
	`\nvidia\installer`,
	`\amd\installer`,
	`\intel\driver`,
}

var defaultDriverRoots = []string{
	`c:\nvidia\`,
	`c:\amd\`,
}

// 关联扩展名表
var keywordExtMap = map[string][]string{
	"image": {".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".bmp"},
	"doc":   {".doc", ".docx", ".pdf", ".txt", ".md", ".xlsx", ".pptx", ".csv"},
	"code":  {".go", ".cpp", ".c", ".h", ".py", ".java", ".js", ".ts", ".json", ".yaml", ".yml", ".toml", ".rs"},
	"arch":  {".zip", ".tar", ".gz", ".7z", ".rar"},
}

type FilterEngine struct {
	systemPatterns []string
	maxResults     int
}

func NewFilterEngine(maxResults int) *FilterEngine {
	return &FilterEngine{
		systemPatterns: defaultSystemPatterns,
		maxResults:     maxResults,
	}
}

func (fe *FilterEngine) Filter(results []ResultSearch, keyword string) []ResultSearch {
	if len(results) == 0 {
		return nil
	}

	cleanKeyword := strings.ToLower(strings.TrimSpace(keyword))
	targetExts := interExtensions(cleanKeyword)
	now := time.Now()

	// 1. 原地过滤 (In-place Filtering)，避免切片扩容开销
	n := 0
	seenPaths := make(map[string]int, len(results))
	for _, r := range results {
		// 排除系统或隐藏路径（直接使用 FullPath 校验）
		if fe.isSystemPath(r.FullPath) {
			continue
		}
		pathKey := normalizeSearchPath(r.FullPath)
		if index, exists := seenPaths[pathKey]; exists {
			// 同一文件既被 Everything 找到又位于 PATH 时，保留 PATH 的排序优势。
			if r.IsPathEntry {
				results[index].IsPathEntry = true
			}
			continue
		}

		// 如果推断出了文件类型（如搜 image），则只保留匹配后缀的文件或文件夹本身
		if len(targetExts) > 0 && !r.IsFolder {
			ext := getExtension(r.FileName)
			if !containsExt(targetExts, ext) {
				continue
			}
		}

		results[n] = r
		seenPaths[pathKey] = n
		n++
	}
	filtered := results[:n]

	// 2. 智能打分与排序
	sort.SliceStable(filtered, func(i, j int) bool {
		scoreI := calculateScore(filtered[i], cleanKeyword, now)
		scoreJ := calculateScore(filtered[j], cleanKeyword, now)

		if scoreI != scoreJ {
			return scoreI > scoreJ // 降序：高分在前
		}
		// 分数相同：文件夹优先于文件
		if filtered[i].IsFolder != filtered[j].IsFolder {
			return filtered[i].IsFolder
		}
		// 依然相同：最近修改的优先
		return filtered[i].ModifiedTime.After(filtered[j].ModifiedTime)
	})

	// 3. 截断安全控制
	if fe.maxResults > 0 && len(filtered) > fe.maxResults {
		return filtered[:fe.maxResults]
	}
	return filtered
}

func (fe *FilterEngine) isSystemPath(fullPath string) bool {
	lowerPath := normalizeSearchPath(fullPath)
	for _, pattern := range fe.systemPatterns {
		if strings.HasPrefix(lowerPath, pattern) {
			return true
		}
	}
	for _, root := range defaultDriverRoots {
		if strings.HasPrefix(lowerPath, root) {
			return true
		}
	}
	for _, pattern := range defaultJunkPathSegments {
		if strings.Contains(lowerPath, pattern) {
			return true
		}
	}
	for _, pattern := range defaultDriverPathSegments {
		if strings.Contains(lowerPath, pattern) {
			return true
		}
	}
	return false
}

func normalizeSearchPath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	path = strings.ReplaceAll(path, "/", `\`)
	if !strings.HasSuffix(path, `\`) {
		path += `\`
	}
	return path
}

// calculateScore 计算结果的关联度分数
func calculateScore(r ResultSearch, keyword string, now time.Time) int {
	score := 0

	// 分别获取文件名与关键字的小写
	lowerFileName := strings.ToLower(r.FileName)
	lowerKeyword := strings.ToLower(keyword)
	lowerPath := strings.ToLower(r.FullPath)

	// 1. 剥离后缀获取 BaseName（仅针对文件）
	baseName := lowerFileName
	if !r.IsFolder {
		if idx := strings.LastIndex(lowerFileName, "."); idx != -1 {
			baseName = lowerFileName[:idx]
		}
	}

	// 2. 维度一：名称匹配度（区分纯文件名与带后缀名）
	if baseName == lowerKeyword {
		score += 100 // 主文件名完全一致（如 logo.png 匹配 logo）
		if r.IsFolder {
			score += 15
		}
	} else if lowerFileName == lowerKeyword {
		score += 90 // 全名完全一致
	} else if strings.HasPrefix(baseName, lowerKeyword) {
		score += 70 // 前缀匹配（如 logo16.png）
	} else if strings.Contains(baseName, lowerKeyword) {
		score += 40 // 包含匹配
	} else if strings.Contains(lowerFileName, lowerKeyword) {
		score += 20 // 仅后缀或整体包含
	}

	// 3. 维度二：路径类型加减分（解决厂商 Logo 的核心）
	// (1) 识别软件内置资源/第三方库路径，直接打入“冷宫”
	if isVendorResourcePath(lowerPath) {
		score -= 80 // 扣除大分，让第三方软件资源排到最后
	}
	if isApplicationInstallPath(lowerPath) && isLaunchable(r) {
		score += 45 // 软件安装位置中的启动入口应优先于同名资源
	}
	if r.IsPathEntry {
		score += 120 // PATH 中可直接执行的命令，是启动器最有价值的结果之一
	}

	// (2) 用户数据盘/常用目录提权
	if isUserPrimaryPath(r.Path) {
		score += 30 // 用户自己的盘符（如 E:\, D:\）或桌面/下载/文档目录
	}

	// 4. 维度三：边际递减的路径深度惩罚（代替线性的 depth * 15）
	// 深度 1-3 层正常扣分，超过 5 层后扣分边际递减，封顶 -25 分
	depth := strings.Count(r.Path, string(os.PathSeparator))
	depthPenalty := depth * 3
	if depthPenalty > 25 {
		depthPenalty = 25
	}
	score -= depthPenalty

	// 5. 维度四：修改时间活跃度加分
	hoursSinceMod := now.Sub(r.ModifiedTime).Hours()
	if hoursSinceMod < 24 {
		score += 15
	} else if hoursSinceMod < 24*7 {
		score += 8
	}

	return score
}

// 辅助函数：判断是否为软件/第三方库的内置资源路径
func isVendorResourcePath(lowerPath string) bool {
	vendorPatterns := []string{
		`\resources\`,
		`\assets\`,
		`\node_modules\`,
		`\vendor\`,
		`\site-packages\`,
		`\target\`,
		`\obj\`,
	}
	for _, p := range vendorPatterns {
		if strings.Contains(lowerPath, p) {
			return true
		}
	}
	return false
}

func isApplicationInstallPath(lowerPath string) bool {
	installRoots := []string{
		`c:\program files\`,
		`c:\program files (x86)\`,
		`c:\programs file\`,
		`c:\programs files\`,
		`\appdata\local\programs\`,
		`\appdata\roaming\`,
	}
	for _, root := range installRoots {
		if strings.Contains(lowerPath, root) {
			return true
		}
	}
	return false
}

func isLaunchable(r ResultSearch) bool {
	if r.IsFolder {
		return true
	}
	switch strings.ToLower(getExtension(r.FileName)) {
	case ".exe", ".com", ".bat", ".cmd", ".lnk", ".url":
		return true
	default:
		return false
	}
}

// 辅助函数：判断是否为用户主导的常用路径
func isUserPrimaryPath(path string) bool {
	if len(path) < 2 {
		return false
	}
	// 非 C 盘的根目录及前几层（如 E:\logo, D:\Projects）
	driver := strings.ToUpper(path[:2])
	if driver != "C:" {
		return true
	}

	// C 盘下的用户核心文件夹
	lowerPath := strings.ToLower(path)
	userDirs := []string{`\desktop`, `\downloads`, `\documents`, `\pictures`, `\code`, `\projects`}
	for _, dir := range userDirs {
		if strings.Contains(lowerPath, dir) {
			return true
		}
	}
	return false
}

func interExtensions(keyword string) []string {
	for domain, exts := range keywordExtMap {
		if strings.Contains(keyword, domain) {
			return exts
		}
	}
	return nil
}

func containsExt(exts []string, target string) bool {
	for _, e := range exts {
		if target == e {
			return true
		}
	}
	return false
}

// getExtension 手动获取后缀，比 filepath.Ext 更轻量且直接转小写
func getExtension(fileName string) string {
	idx := strings.LastIndex(fileName, ".")
	if idx == -1 {
		return ""
	}
	return strings.ToLower(fileName[idx:])
}
