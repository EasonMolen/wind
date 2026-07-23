//go:build windows

package icon

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png" // 必须显式引入 png 解码器，否则 image.Decode 会失败
	"path/filepath"
	"syscall"
	"unsafe"
)

// ============================================================================
// 1. Win32 常量与结构体定义 (零 CGO 纯 Syscall 绑定)
// ============================================================================

const (
	SHGFI_ICON              = 0x000000100 // 获取 HICON 句柄
	SHGFI_DISPLAYNAME       = 0x000000200
	SHGFI_TYPENAME          = 0x000000400 // 获取类型名称 (如 "Microsoft Word 文档")
	SHGFI_LARGEICON         = 0x000000000 // 32x32
	SHGFI_SMALLICON         = 0x000000001 // 16x16
	SHGFI_USEFILEATTRIBUTES = 0x000000010 // 伪造文件路径标志位
	SHGFI_SYSICONINDEX      = 0x000004000 // 获取系统图标列表中索引 (用于高分辨率提取)

	FILE_ATTRIBUTE_NORMAL    = 0x00000080
	FILE_ATTRIBUTE_DIRECTORY = 0x00000010

	// Shell Image List 尺寸定义 (用于获取 48x48 和 256x256 特大高清图标)
	SHIL_LARGE      = 0 // 32x32
	SHIL_SMALL      = 1 // 16x16
	SHIL_EXTRALARGE = 2 // 48x48
	SHIL_JUMBO      = 4 // 256x256
)

// SHFILEINFOW 操作系统文件信息结构体
type SHFILEINFOW struct {
	HIcon       syscall.Handle
	IIcon       int32
	Attributes  uint32
	DisplayName [260]uint16
	TypeName    [80]uint16
}

// GdiplusStartupInput GDI+ 初始化参数
type GdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

var (
	// Windows 系统 DLL 延迟加载
	modshell32  = syscall.NewLazyDLL("shell32.dll")
	moduser32   = syscall.NewLazyDLL("user32.dll")
	modgdiplus  = syscall.NewLazyDLL("gdiplus.dll")
	modole32    = syscall.NewLazyDLL("ole32.dll")
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	// Shell32 API
	procSHGetFileInfoW = modshell32.NewProc("SHGetFileInfoW")
	procSHGetImageList = modshell32.NewProc("SHGetImageList")

	// User32 API
	procDestroyIcon = moduser32.NewProc("DestroyIcon")

	// GDI+ API
	procGdiplusStartup            = modgdiplus.NewProc("GdiplusStartup")
	procGdipCreateBitmapFromHICON = modgdiplus.NewProc("GdipCreateBitmapFromHICON")
	procGdipSaveImageToStream     = modgdiplus.NewProc("GdipSaveImageToStream")
	procGdipDisposeImage          = modgdiplus.NewProc("GdipDisposeImage")

	// Ole32 API
	procCreateStreamOnHGlobal = modole32.NewProc("CreateStreamOnHGlobal")
	procGetHGlobalFromStream  = modole32.NewProc("GetHGlobalFromStream")

	// Kernel32 API
	procGlobalLock   = modkernel32.NewProc("GlobalLock")
	procGlobalSize   = modkernel32.NewProc("GlobalSize")
	procGlobalUnlock = modkernel32.NewProc("GlobalUnlock")

	// IImageList COM 接口 GUID
	IID_IImageList = GUID{0x46EB5926, 0x582E, 0x4017, [8]byte{0x9F, 0xDF, 0xE8, 0x99, 0x8D, 0xAA, 0x09, 0x50}}

	// GDI+ 全局初始化 Token
	gdiplusToken uintptr
)

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func init() {
	// 初始化 GDI+ 环境 (用于将 HICON 转换为 PNG)
	var input GdiplusStartupInput
	input.GdiplusVersion = 1
	procGdiplusStartup.Call(
		uintptr(unsafe.Pointer(&gdiplusToken)),
		uintptr(unsafe.Pointer(&input)),
		0,
	)
}

// ============================================================================
// 2. Windows Fetcher 结构定义与接口实现
// ============================================================================

type winFetcher struct{}

func newPlatformFetcher() Fetcher {
	return &winFetcher{}
}

// FetchByPath 从物理磁盘文件路径提取图标
func (f *winFetcher) FetchByPath(ctx context.Context, path string, size IconSize) (*IconResult, error) {
	cleanPath := filepath.Clean(path)
	pathPtr, err := syscall.UTF16PtrFromString(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("icon: invalid path encoding: %w", err)
	}

	return f.extractIcon(ctx, uintptr(unsafe.Pointer(pathPtr)), 0, false, size)
}

// FetchByExtension 根据扩展名/通用类型提取图标 (无需磁盘存在文件)
func (f *winFetcher) FetchByExtension(ctx context.Context, ext string, size IconSize) (*IconResult, error) {
	var fakePath string
	var dwFileAttributes uint32 = FILE_ATTRIBUTE_NORMAL

	if ext == ":folder:" {
		fakePath = "dummy_folder"
		dwFileAttributes = FILE_ATTRIBUTE_DIRECTORY
	} else {
		fakePath = "dummy" + ext
	}

	pathPtr, err := syscall.UTF16PtrFromString(fakePath)
	if err != nil {
		return nil, fmt.Errorf("icon: invalid extension encoding: %w", err)
	}

	return f.extractIcon(ctx, uintptr(unsafe.Pointer(pathPtr)), dwFileAttributes, true, size)
}

// ============================================================================
// 3. 核心 API 调用的内部实现
// ============================================================================

func (f *winFetcher) extractIcon(ctx context.Context, pszPath uintptr, dwFileAttributes uint32, useAttributes bool, size IconSize) (*IconResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var shfi SHFILEINFOW
	flags := uint32(SHGFI_ICON | SHGFI_TYPENAME)

	if useAttributes {
		flags |= SHGFI_USEFILEATTRIBUTES
	}

	// 特大图标 (48x48 或 256x256) 优先采用高级路线：提取系统的 ImageList 句柄
	if size == SizeLarge || size == SizeExtraLarge {
		hIcon, typeName, err := f.getJumboIcon(pszPath, dwFileAttributes, flags, size)
		if err == nil && hIcon != 0 {
			defer destroyIcon(hIcon)
			return f.convertHIconToResult(hIcon, typeName, size)
		}
		// 高清提取失败自动降级到普通 API
	}

	// 常规 16x16 / 32x32 路线
	if size == SizeSmall {
		flags |= SHGFI_SMALLICON
	} else {
		flags |= SHGFI_LARGEICON
	}

	ret, _, _ := procSHGetFileInfoW.Call(
		pszPath,
		uintptr(dwFileAttributes),
		uintptr(unsafe.Pointer(&shfi)),
		uintptr(unsafe.Sizeof(shfi)),
		uintptr(flags),
	)

	if ret == 0 || shfi.HIcon == 0 {
		return nil, ErrIconNotFound
	}

	defer destroyIcon(shfi.HIcon)

	typeName := syscall.UTF16ToString(shfi.TypeName[:])
	return f.convertHIconToResult(shfi.HIcon, typeName, size)
}

// getJumboIcon 使用 SHGetImageList 提取 48x48 或 256x256 特大无损图标
func (f *winFetcher) getJumboIcon(pszPath uintptr, dwFileAttributes uint32, baseFlags uint32, size IconSize) (syscall.Handle, string, error) {
	var shfi SHFILEINFOW
	flags := baseFlags | SHGFI_SYSICONINDEX

	ret, _, _ := procSHGetFileInfoW.Call(
		pszPath,
		uintptr(dwFileAttributes),
		uintptr(unsafe.Pointer(&shfi)),
		uintptr(unsafe.Sizeof(shfi)),
		uintptr(flags),
	)

	if ret == 0 {
		return 0, "", ErrIconNotFound
	}

	var shilType uintptr = SHIL_JUMBO
	if size == SizeLarge {
		shilType = SHIL_EXTRALARGE
	}

	var imageList uintptr
	r1, _, _ := procSHGetImageList.Call(
		shilType,
		uintptr(unsafe.Pointer(&IID_IImageList)),
		uintptr(unsafe.Pointer(&imageList)),
	)

	// S_OK 在 Win32 API 中的返回值就是 0
	if r1 != 0 || imageList == 0 {
		return 0, "", ErrIconNotFound
	}

	// 通过 COM 接口 IImageList::GetIcon(iIndex, flags, &hIcon) 获取高分辨率 HICON
	// 虚表 GetIcon 偏移为第 10 个函数索引
	var hIcon syscall.Handle
	vtable := **(**uintptr)(unsafe.Pointer(imageList))
	getIconProc := *(*uintptr)(unsafe.Pointer(vtable + 10*unsafe.Sizeof(uintptr(0))))

	rGetIcon, _, _ := syscall.SyscallN(
		getIconProc,
		imageList,
		uintptr(shfi.IIcon),
		0, // ILD_NORMAL
		uintptr(unsafe.Pointer(&hIcon)),
	)

	// 释放 IImageList COM 引用 (Release 为第 2 个函数)
	releaseProc := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
	syscall.SyscallN(releaseProc, imageList)

	if rGetIcon != 0 || hIcon == 0 {
		return 0, "", ErrIconNotFound
	}

	typeName := syscall.UTF16ToString(shfi.TypeName[:])
	return hIcon, typeName, nil
}

// ============================================================================
// 4. HICON 内存转换与格式转换器 (Win32 HICON -> PNG Bytes & image.Image)
// ============================================================================

func (f *winFetcher) convertHIconToResult(hIcon syscall.Handle, typeName string, size IconSize) (*IconResult, error) {
	var gpBitmap uintptr
	r1, _, _ := procGdipCreateBitmapFromHICON.Call(
		uintptr(hIcon),
		uintptr(unsafe.Pointer(&gpBitmap)),
	)

	// GDI+ 函数成功返回值为 0 (Ok)
	if r1 != 0 || gpBitmap == 0 {
		return nil, fmt.Errorf("icon: GDI+ failed to convert HICON, status: %d", r1)
	}
	defer procGdipDisposeImage.Call(gpBitmap)

	pngBytes, err := saveGpBitmapToPNG(gpBitmap)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("icon: failed to decode PNG image: %w", err)
	}

	return &IconResult{
		Image:    img,
		PNGBytes: pngBytes,
		TypeName: typeName,
		Size:     size,
	}, nil
}

func destroyIcon(hIcon syscall.Handle) {
	if hIcon != 0 {
		procDestroyIcon.Call(uintptr(hIcon))
	}
}

// saveGpBitmapToPNG 利用 GDI+ API 将 GpBitmap 保存为 PNG 字节流
func saveGpBitmapToPNG(gpBitmap uintptr) ([]byte, error) {
	// PNG 编码器的 CLSID: {557CF406-1A04-11D3-9A73-0000F81EF32E}
	clsidPNG := GUID{
		Data1: 0x557cf406,
		Data2: 0x1a04,
		Data3: 0x11d3,
		Data4: [8]byte{0x9a, 0x73, 0x00, 0x00, 0xf8, 0x1e, 0xf3, 0x2e},
	}

	var stream uintptr
	// CreateStreamOnHGlobal 成功返回 0 (S_OK)
	r1, _, _ := procCreateStreamOnHGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream)))
	if r1 != 0 || stream == 0 {
		return nil, fmt.Errorf("icon: failed to create IStream, hresult: %d", r1)
	}

	vtable := **(**uintptr)(unsafe.Pointer(stream))
	releaseProc := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
	defer syscall.SyscallN(releaseProc, stream)

	rSaved, _, _ := procGdipSaveImageToStream.Call(
		gpBitmap,
		stream,
		uintptr(unsafe.Pointer(&clsidPNG)),
		0,
	)

	if rSaved != 0 {
		return nil, fmt.Errorf("icon: failed to save bitmap to PNG stream, status: %d", rSaved)
	}

	var hGlobal uintptr
	rGet, _, _ := procGetHGlobalFromStream.Call(stream, uintptr(unsafe.Pointer(&hGlobal)))
	if rGet != 0 || hGlobal == 0 {
		return nil, fmt.Errorf("icon: failed to get HGlobal from IStream")
	}

	ptr, _, _ := procGlobalLock.Call(hGlobal)
	if ptr == 0 {
		return nil, fmt.Errorf("icon: failed to lock global stream handle")
	}
	defer procGlobalUnlock.Call(hGlobal)

	size, _, _ := procGlobalSize.Call(hGlobal)
	if size == 0 {
		return nil, fmt.Errorf("icon: empty PNG stream size")
	}

	buf := make([]byte, size)
	// 内存安全安全地拷贝到 Go 托管内存
	copy(buf, (*[1 << 30]byte)(unsafe.Pointer(ptr))[:size:size])

	return buf, nil
}
