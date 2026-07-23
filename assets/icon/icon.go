package icon

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrIconNotFound        = errors.New("icon: icon not found or resource unreadable")
	ErrInvalidPath         = errors.New("icon: invalid path or empty file name")
	ErrUnsupportedPlatform = errors.New("icon: unsupported operating system")
)

// IconSize 代表图标尺寸，推荐采用 Windows Shell 标准尺寸定义
type IconSize int

const (
	// SizeSmall 16x16 (列表视图/详细信息)
	SizeSmall IconSize = 16
	// SizeMedium 32x32 (平铺/小图标)
	SizeMedium IconSize = 32
	// SizeLarge 48x48 (中等图标)
	SizeLarge IconSize = 48
	// SizeExtraLarge 256x256 (特大图标/高清缩略图级别)
	SizeExtraLarge IconSize = 256
)

// Format 指定导出的图像数据格式
type Format int

const (
	FormatPNG Format = iota + 1
	FormatRGBA
)

type Config struct {
	// CacheCapacity 内存缓存最大条目数，0 表示不限制，默认 1000
	CacheCapacity int
	// DefaultTimeout 单次获取图标的超时时间，默认 3 秒
	DefaultTimeout time.Duration
	// EnableExtRouting 是否开启针对普通扩展名的自动降级/快速提取（如 .docx 伪造路径）
	EnableExtRouting bool
}

func DefaultConfig() Config {
	return Config{
		CacheCapacity:    1024,
		DefaultTimeout:   3 * time.Second,
		EnableExtRouting: true,
	}
}

type IconResult struct {
	Image    image.Image // 解码后的 image.Image 对象
	PNGBytes []byte      // PNG 格式字节流
	TypeName string      // 操作系统内部的文件类型描述 (例如: "Microsoft Word 文档")
	Size     IconSize    // 实际返回的尺寸
}

// Fetcher 是底层的操作系统图标提取接口，由 fetcher_win.go 实现
type Fetcher interface {
	// FetchByPath 通过路径获取图标
	FetchByPath(ctx context.Context, path string, size IconSize) (*IconResult, error)
	// FetchByExtension 通过后缀获取图标
	FetchByExtension(ctx context.Context, ext string, size IconSize) (*IconResult, error)
}

type Cache interface {
	Get(query string, size IconSize) (*IconResult, bool)
	Set(query string, size IconSize, val *IconResult)
	Delete(query string, size IconSize)
	Clear()
	Len() int
}

// Engine 图标提取核心引擎
type Engine struct {
	cfg     Config
	fetcher Fetcher // 唯一持有的抽象层，外部既不需要知道平台差异，也不需要知道是否有缓存
}

var (
	defaultEngine *Engine
	once          sync.Once
)

func InitGlobalEngine(cfg Config) {
	once.Do(func() {
		defaultEngine = NewEngine(cfg)
	})
}

// GlobalEngine 获取默认全局引擎（若未显式 InitGlobal 则使用 DefaultConfig）
func GlobalEngine() *Engine {
	if defaultEngine == nil {
		InitGlobalEngine(DefaultConfig())
	}
	return defaultEngine
}

// NewEngine 创建独立的图标提取引擎
func NewEngine(cfg Config) *Engine {
	// 创建操作系统底层的真实提取器 (Windows/Mac/Linux 自动编译对应底层)
	rawFetcher := newPlatformFetcher()

	// 初始化 LRU+TTL 缓存层
	lruCache := NewLRUCache(cfg.CacheCapacity)

	// 将两者结合：让 Fetcher 具备内存缓存能力
	// 此时赋予 e.fetcher 的是一个既跨平台、又有自动缓存拦截能力的超级接口！
	return &Engine{
		cfg:     cfg,
		fetcher: NewCachedFetcher(rawFetcher, lruCache),
	}
}

// ============================================================================
// 文件夹和扩展名字典预加载
// ============================================================================

var folderKeywords = map[string]struct{}{
	"folder": {}, "dir": {}, "directory": {}, "folder_open": {},
}

var standaloneExtensions = map[string]struct{}{
	".exe": {}, ".dll": {}, ".ico": {}, ".lnk": {},
	".cpl": {}, ".msc": {}, ".scr": {}, ".sys": {},
	".cur": {}, ".ani": {},
}

// Get 根据文件路径或文件名获取图标
func (e *Engine) Get(ctx context.Context, path string, size IconSize) (*IconResult, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, ErrInvalidPath
	}

	cleanPath = filepath.Clean(cleanPath)
	ext := strings.ToLower(filepath.Ext(cleanPath))

	_, isStandaloneExt := standaloneExtensions[ext]
	isNoExt := ext == ""

	// 如果开启了扩展名路由，且不是可执行/快捷方式类专属图标，走极速扩展名方案
	if e.cfg.EnableExtRouting && !isStandaloneExt && !isNoExt {
		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			return e.GetByExtension(ctx, ext, size)
		}
		return e.GetByExtension(ctx, ext, size)
	}

	// 统一控制超时时间（仅当上一层没有设置 Deadline 时生效）
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok && e.cfg.DefaultTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.cfg.DefaultTimeout)
		defer cancel()
	}

	// 直接调用抽象接口。
	// 【魔法触发点】：如果之前的查询还在缓存中，e.fetcher 会在原地返回！
	res, err := e.fetcher.FetchByPath(ctx, cleanPath, size)
	if err != nil {
		// 防御性降级：如果本地的这个 exe/dll 损坏或读取失败，退化为它的通向后缀名图标
		if ext != "" {
			return e.GetByExtension(ctx, ext, size)
		}
		return nil, err
	}

	return res, nil
}

// GetByExtension 直接根据文件扩展名提取通用图标 (例如 ".docx", "pdf", "folder", ".go")
func (e *Engine) GetByExtension(ctx context.Context, ext string, size IconSize) (*IconResult, error) {
	cleanExt := strings.ToLower(strings.TrimSpace(ext))

	// 语义化与格式规范
	if _, isFolderKey := folderKeywords[cleanExt]; isFolderKey {
		cleanExt = ":folder:" // 使用专用的内部命名空间作为虚拟后缀
	} else {
		cleanExt = strings.TrimLeft(cleanExt, ".")
		if cleanExt == "" {
			cleanExt = ".file" // 归一化为通用未知文件类型
		} else {
			cleanExt = "." + cleanExt
		}
	}

	// 统一控制超时时间
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok && e.cfg.DefaultTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.cfg.DefaultTimeout)
		defer cancel()
	}

	// 直接调用！如果缓存里没有，底层才会真正调用 API
	res, err := e.fetcher.FetchByExtension(ctx, cleanExt, size)
	if err != nil {
		// 防御性兜底：遇到系统完全不认识的奇怪扩展名导致报错，降级使用通用未命名图标 (.file)
		if cleanExt != ".file" && cleanExt != ":folder:" {
			return e.GetByExtension(ctx, ".file", size)
		}
		return nil, err
	}

	return res, nil
}

// -----------------------------------------------------------------------------
// 快捷包级函数（直接调用全局单例）
// -----------------------------------------------------------------------------

// GetPNG 获取指定路径图标的 PNG 字节流
func GetPNG(path string, size IconSize) ([]byte, error) {
	res, err := GlobalEngine().Get(context.Background(), path, size)
	if err != nil {
		return nil, err
	}
	return res.PNGBytes, nil
}

// GetPNGByExt 根据扩展名获取图标 PNG 字节流
func GetPNGByExt(ext string, size IconSize) ([]byte, error) {
	res, err := GlobalEngine().GetByExtension(context.Background(), ext, size)
	if err != nil {
		return nil, err
	}
	return res.PNGBytes, nil
}

// GetImage 获取 `image.Image` 对象
func GetImage(path string, size IconSize) (image.Image, error) {
	res, err := GlobalEngine().Get(context.Background(), path, size)
	if err != nil {
		return nil, err
	}
	return res.Image, nil
}
